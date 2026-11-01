// Package tagging 封面处理与音乐标签写入
package tagging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"

	_ "golang.org/x/image/webp"

	marker "github.com/XiaoMengXinX/163KeyMarker"
	"github.com/bogem/id3v2"
	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
	"github.com/nfnt/resize"
)

// Metadata 是通用 ID3/FLAC 标签内容。
type Metadata struct {
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
	Album   string   `json:"album"`
}

const (
	maxCoverBytes  = 32 << 20
	maxCoverPixels = 4096 * 4096
)

// ResizeCover 缩放封面到 320x320（黑底填充），输出 *.resize.jpg
func ResizeCover(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	// 封面来自插件，限制文件大小与像素数，避免解压炸弹；用 LimitReader 而非先全量读入
	buffer, err := io.ReadAll(io.LimitReader(file, maxCoverBytes+1))
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(buffer) > maxCoverBytes {
		return "", fmt.Errorf("封面文件过大（超过 %d 字节）: %s", maxCoverBytes, filePath)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(buffer))
	if err != nil {
		return "", fmt.Errorf("image decode error %s: %w", filePath, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxCoverPixels {
		return "", fmt.Errorf("封面尺寸异常 %dx%d: %s", cfg.Width, cfg.Height, filePath)
	}

	img, _, err := image.Decode(bytes.NewReader(buffer))
	if err != nil {
		return "", fmt.Errorf("image decode error %s: %w", filePath, err)
	}

	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	var m image.Image
	if width/height >= 1 {
		m = resize.Resize(320, uint(height)*320/uint(width), img, resize.Lanczos3)
	} else {
		m = resize.Resize(uint(width*320/height), 320, img, resize.Lanczos3)
	}

	newImag := image.NewNRGBA(image.Rect(0, 0, 320, 320))
	if m.Bounds().Dx() > m.Bounds().Dy() {
		draw.Draw(newImag, image.Rectangle{
			Min: image.Point{Y: (320 - m.Bounds().Dy()) / 2},
			Max: image.Point{X: 320, Y: 320},
		}, m, m.Bounds().Min, draw.Src)
	} else {
		draw.Draw(newImag, image.Rectangle{
			Min: image.Point{X: (320 - m.Bounds().Dx()) / 2},
			Max: image.Point{X: 320, Y: 320},
		}, m, m.Bounds().Min, draw.Src)
	}

	outPath := filePath + ".resize.jpg"
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	if err := jpeg.Encode(out, newImag, nil); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return outPath, nil
}

// Write 按 provider 声明的 TagFormat 写入标签；空格式表示跳过。
func Write(filePath, picPath, format string, payload json.RawMessage) error {
	switch format {
	case "":
		return nil
	case "netease163key":
		var mark marker.MarkerData
		if err := json.Unmarshal(payload, &mark); err != nil {
			return fmt.Errorf("解析 163key 标签失败: %w", err)
		}
		if err := validateMarker(&mark); err != nil {
			return err
		}
		return writeMarker(filePath, picPath, mark)
	case "id3":
		var meta Metadata
		if err := json.Unmarshal(payload, &meta); err != nil {
			return fmt.Errorf("解析 ID3 标签失败: %w", err)
		}
		return writeMetadata(filePath, picPath, meta)
	default:
		return fmt.Errorf("未知标签格式 %q", format)
	}
}

// 163KeyMarker 内部直接断言歌手名，宿主先校验避免插件数据触发 panic。
func validateMarker(mark *marker.MarkerData) error {
	if mark.MusicName == "" {
		return fmt.Errorf("163key 标签缺少歌曲名")
	}
	for _, artist := range mark.Artist {
		if len(artist) == 0 {
			return fmt.Errorf("163key 标签歌手格式错误")
		}
		if _, ok := artist[0].(string); !ok {
			return fmt.Errorf("163key 标签歌手名格式错误")
		}
	}
	return nil
}

// 带封面失败时降级为无封面重试
func writeMarker(filePath, picPath string, mark marker.MarkerData) error {
	musicFile, err := os.Open(filePath)
	if err != nil {
		return err
	}
	err = writeMarkerFile(musicFile, picPath, mark)
	_ = musicFile.Close()
	if err == nil || picPath == "" {
		return err
	}

	musicFile, err = os.Open(filePath)
	if err != nil {
		return err
	}
	err = writeMarkerFile(musicFile, "", mark)
	_ = musicFile.Close()
	return err
}

func writeMarkerFile(musicFile *os.File, picPath string, mark marker.MarkerData) error {
	if picPath == "" {
		return marker.AddMusicID3V2(musicFile, nil, mark)
	}
	picFile, err := os.Open(picPath)
	if err != nil {
		return err
	}
	defer picFile.Close()
	return marker.AddMusicID3V2(musicFile, picFile, mark)
}

func writeMetadata(filePath, picPath string, meta Metadata) error {
	switch {
	case strings.HasSuffix(strings.ToLower(filePath), ".mp3"):
		return writeMP3(filePath, picPath, meta)
	case strings.HasSuffix(strings.ToLower(filePath), ".flac"):
		return writeFLAC(filePath, picPath, meta)
	default:
		return nil
	}
}

func writeMP3(filePath, picPath string, meta Metadata) error {
	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetTitle(meta.Title)
	tag.SetArtist(strings.Join(meta.Artists, ", "))
	if meta.Album != "" {
		tag.SetAlbum(meta.Album)
	}
	if picPath != "" {
		artwork, err := os.ReadFile(picPath)
		if err != nil {
			return err
		}
		tag.AddAttachedPicture(id3v2.PictureFrame{
			Encoding:    id3v2.EncodingISO,
			MimeType:    imageMime(artwork),
			PictureType: id3v2.PTFrontCover,
			Description: "Front cover",
			Picture:     artwork,
		})
	}
	return tag.Save()
}

func writeFLAC(filePath, picPath string, meta Metadata) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	parsed, err := flac.ParseMetadata(file)
	if err != nil {
		return err
	}

	if picPath != "" {
		artwork, err := os.ReadFile(picPath)
		if err != nil {
			return err
		}
		picture, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "Front cover", artwork, imageMime(artwork))
		if err == nil {
			block := picture.Marshal()
			parsed.Meta = append(parsed.Meta, &block)
		}
	}

	tag := flacvorbis.New()
	_ = tag.Add(flacvorbis.FIELD_TITLE, meta.Title)
	if len(meta.Artists) != 0 {
		_ = tag.Add(flacvorbis.FIELD_ARTIST, strings.Join(meta.Artists, ", "))
	}
	if meta.Album != "" {
		_ = tag.Add(flacvorbis.FIELD_ALBUM, meta.Album)
	}
	tagBlock := tag.Marshal()
	replaced := false
	for i, block := range parsed.Meta {
		if block.Type == flac.VorbisComment {
			parsed.Meta[i] = &tagBlock
			replaced = true
			break
		}
	}
	if !replaced {
		parsed.Meta = append(parsed.Meta, &tagBlock)
	}

	stat, err := file.Stat()
	if err != nil {
		return err
	}
	tmpPath := filePath + "-id3v2"
	tmp, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, stat.Mode())
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write([]byte("fLaC")); err != nil {
		_ = tmp.Close()
		return err
	}
	for i, block := range parsed.Meta {
		if _, err := tmp.Write(block.Marshal(i == len(parsed.Meta)-1)); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := io.Copy(tmp, file); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return err
	}
	remove = false
	return nil
}

func imageMime(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}
	if len(data) > 32 {
		data = data[:32]
	}
	return http.DetectContentType(data)
}
