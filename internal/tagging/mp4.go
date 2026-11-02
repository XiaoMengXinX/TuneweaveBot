package tagging

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/Eyevinn/mp4ff/mp4"
)

type topBox struct {
	name      string
	off, size uint64
}

// writeMP4 把标签写进 moov/udta/meta/ilst。moov 变大后，位于其后的 mdat 被整体后移，
// 因此同步修正 stco/co64 与显式 base_data_offset 的 tfhd；其余盒子原样拷贝。
func writeMP4(filePath, picPath string, meta Metadata) error {
	src, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer src.Close()

	boxes, err := scanTopBoxes(src)
	if err != nil {
		return err
	}
	moovIdx := -1
	for i, b := range boxes {
		if b.name == "moov" {
			moovIdx = i
			break
		}
	}
	if moovIdx < 0 {
		return errors.New("mp4 文件缺少 moov")
	}
	moov, err := decodeAt(src, boxes[moovIdx])
	if err != nil {
		return err
	}
	moovBox := moov.(*mp4.MoovBox)

	ilst, err := buildIlst(picPath, meta)
	if err != nil {
		return err
	}
	setIlst(moovBox, ilst)

	delta := int64(moovBox.Size()) - int64(boxes[moovIdx].size)
	if err := shiftChunkOffsets(moovBox, boxes[moovIdx].off, delta); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".tag-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	for i, b := range boxes {
		switch {
		case i == moovIdx:
			err = moovBox.Encode(tmp)
		case b.name == "moof" && i > moovIdx:
			err = copyMoof(tmp, src, b, delta)
		default:
			err = copyRange(tmp, src, b)
		}
		if err != nil {
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filePath)
}

func scanTopBoxes(f *os.File) ([]topBox, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := uint64(st.Size())
	var boxes []topBox
	for off := uint64(0); off < fileSize; {
		if _, err := f.Seek(int64(off), io.SeekStart); err != nil {
			return nil, err
		}
		hdr, err := mp4.DecodeHeader(f)
		if err != nil {
			return nil, fmt.Errorf("解析 mp4 盒子头失败: %w", err)
		}
		size := hdr.Size
		if size == 0 { // 延伸到文件末尾
			size = fileSize - off
		}
		if size < uint64(hdr.Hdrlen) || off+size > fileSize {
			return nil, fmt.Errorf("mp4 盒子 %q 大小异常", hdr.Name)
		}
		boxes = append(boxes, topBox{name: hdr.Name, off: off, size: size})
		off += size
	}
	return boxes, nil
}

func decodeAt(f *os.File, b topBox) (mp4.Box, error) {
	if _, err := f.Seek(int64(b.off), io.SeekStart); err != nil {
		return nil, err
	}
	return mp4.DecodeBox(b.off, io.LimitReader(f, int64(b.size)))
}

func copyRange(w io.Writer, f *os.File, b topBox) error {
	_, err := io.Copy(w, io.NewSectionReader(f, int64(b.off), int64(b.size)))
	return err
}

func copyMoof(w io.Writer, f *os.File, b topBox, delta int64) error {
	box, err := decodeAt(f, b)
	if err != nil {
		return err
	}
	moof := box.(*mp4.MoofBox)
	shifted := false
	for _, traf := range moof.Trafs {
		if traf.Tfhd != nil && traf.Tfhd.HasBaseDataOffset() {
			traf.Tfhd.BaseDataOffset = uint64(int64(traf.Tfhd.BaseDataOffset) + delta)
			shifted = true
		}
	}
	if !shifted {
		return copyRange(w, f, b)
	}
	return moof.Encode(w)
}

// shiftChunkOffsets 修正 moov 之后的块偏移；moov 之前的数据位置不变。
func shiftChunkOffsets(moov *mp4.MoovBox, moovOff uint64, delta int64) error {
	for _, trak := range moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		stbl := trak.Mdia.Minf.Stbl
		if stbl.Stco != nil {
			for i, o := range stbl.Stco.ChunkOffset {
				if uint64(o) > moovOff {
					n := int64(o) + delta
					if n < 0 || n > math.MaxUint32 {
						return errors.New("mp4 块偏移超出 stco 范围")
					}
					stbl.Stco.ChunkOffset[i] = uint32(n)
				}
			}
		}
		if stbl.Co64 != nil {
			for i, o := range stbl.Co64.ChunkOffset {
				if o > moovOff {
					stbl.Co64.ChunkOffset[i] = uint64(int64(o) + delta)
				}
			}
		}
	}
	return nil
}

func buildIlst(picPath string, meta Metadata) (*mp4.IlstBox, error) {
	ilst := &mp4.IlstBox{}
	addText := func(name, value string) {
		if value == "" {
			return
		}
		item := mp4.NewGenericContainerBox(name)
		item.AddChild(&mp4.DataBox{DataType: mp4.DataTypeUTF8, Data: []byte(value)})
		ilst.AddChild(item)
	}
	addText("\xa9nam", meta.Title)
	addText("\xa9ART", joinNames(meta.Artists))
	addText("\xa9alb", meta.Album)
	addText("aART", joinNames(meta.AlbumArtists))
	addText("\xa9day", meta.Date)
	addText("\xa9wrt", joinNames(meta.Composers))
	addText("cprt", meta.Copyright)
	addPosition := func(name string, n, total, size int) {
		if n <= 0 {
			return
		}
		// trkn 为 8 字节、disk 为 6 字节：保留(2) 序号(2) 总数(2) [保留(2)]
		payload := make([]byte, size)
		binary.BigEndian.PutUint16(payload[2:], uint16(n))
		binary.BigEndian.PutUint16(payload[4:], uint16(total))
		item := mp4.NewGenericContainerBox(name)
		item.AddChild(&mp4.DataBox{DataType: mp4.DataTypeImplicit, Data: payload})
		ilst.AddChild(item)
	}
	addPosition("trkn", meta.TrackNumber, meta.TrackTotal, 8)
	addPosition("disk", meta.DiscNumber, meta.DiscTotal, 6)
	for _, k := range sortedKeys(meta.Custom) {
		if v := meta.Custom[k]; v != "" {
			ilst.AddChild(freeformItem(k, v))
		}
	}
	if picPath != "" {
		artwork, err := os.ReadFile(picPath)
		if err != nil {
			return nil, err
		}
		dataType := uint32(mp4.DataTypeJPEG)
		if imageMime(artwork) == "image/png" {
			dataType = mp4.DataTypePNG
		}
		covr := mp4.NewGenericContainerBox("covr")
		covr.AddChild(&mp4.DataBox{DataType: dataType, Data: artwork})
		ilst.AddChild(covr)
	}
	return ilst, nil
}

// freeformItem 构造 iTunes 自定义字段 ----:com.apple.iTunes:<name>。
func freeformItem(name, value string) mp4.Box {
	fullBox := func(typ, text string) mp4.Box {
		payload := append(make([]byte, 4), text...) // version/flags 为 0
		return mp4.CreateUnknownBox(typ, uint64(8+len(payload)), payload)
	}
	item := mp4.NewGenericContainerBox("----")
	item.AddChild(fullBox("mean", "com.apple.iTunes"))
	item.AddChild(fullBox("name", name))
	item.AddChild(&mp4.DataBox{DataType: mp4.DataTypeUTF8, Data: []byte(value)})
	return item
}

// setIlst 用新的 iTunes 元数据替换 moov/udta 中已有的 meta。
func setIlst(moov *mp4.MoovBox, ilst *mp4.IlstBox) {
	meta := mp4.CreateMetaBox(0, &mp4.HdlrBox{HandlerType: "mdir", Reserved: [3]uint32{0x6170706c}}) // "appl"
	meta.AddChild(ilst)

	var udta *mp4.UdtaBox
	for _, c := range moov.Children {
		if u, ok := c.(*mp4.UdtaBox); ok {
			udta = u
			break
		}
	}
	if udta == nil {
		udta = &mp4.UdtaBox{}
		moov.AddChild(udta)
	}
	kept := udta.Children[:0]
	for _, c := range udta.Children {
		if c.Type() != "meta" {
			kept = append(kept, c)
		}
	}
	udta.Children = append(kept, meta)
}
