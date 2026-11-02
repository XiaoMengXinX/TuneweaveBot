package tagging

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

// testdata 由 ffmpeg 生成，覆盖分片（default-base-is-moof / 显式 base_data_offset）
// 与非分片（moov 在前 / 在后）四种布局，均带有旧的 title 标签。
var mp4Fixtures = []string{"frag_moof.m4a", "frag_base.m4a", "prog_front.m4a", "prog_end.m4a"}

func TestWriteMP4(t *testing.T) {
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.jpg")
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Metadata{
		Title: "新标题", Artists: []string{"A", "B"}, Album: "专辑", AlbumArtists: []string{"A"},
		Date: "2024-05-01", TrackNumber: 3, TrackTotal: 12, DiscNumber: 1, Composers: []string{"C"},
		Copyright: "℗ 2024 L", Custom: map[string]string{"ISRC": "USRC12400001", "label": "L"},
	}

	for _, name := range mp4Fixtures {
		t.Run(name, func(t *testing.T) {
			orig, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, orig, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Write(path, cover, "id3", mustJSON(t, meta)); err != nil {
				t.Fatal(err)
			}
			tagged, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			items := ilstItems(t, tagged)
			for item, want := range map[string]string{
				"\xa9nam": "新标题", "\xa9ART": "A, B", "\xa9alb": "专辑", "aART": "A",
				"\xa9day": "2024-05-01", "\xa9wrt": "C", "cprt": "℗ 2024 L",
			} {
				if got := string(items[item]); got != want {
					t.Errorf("%q = %q, want %q", item, got, want)
				}
			}
			if got := items["trkn"]; !bytes.Equal(got, []byte{0, 0, 0, 3, 0, 12, 0, 0}) {
				t.Errorf("trkn = %v", got)
			}
			if got := items["covr"]; !bytes.Equal(got, buf.Bytes()) {
				t.Errorf("covr 长度 %d，期望 %d", len(got), buf.Len())
			}
			for k, want := range map[string]string{"----:ISRC": "USRC12400001", "----:label": "L"} {
				if got := string(items[k]); got != want {
					t.Errorf("%q = %q, want %q", k, got, want)
				}
			}
			if got := items["disk"]; !bytes.Equal(got, []byte{0, 0, 0, 1, 0, 0}) {
				t.Errorf("disk = %v", got)
			}

			// 每个数据指针在新文件中都应指向与原文件相同的字节
			before, after := dataPointers(t, orig), dataPointers(t, tagged)
			if len(before) == 0 || len(before) != len(after) {
				t.Fatalf("数据指针数量 %d -> %d", len(before), len(after))
			}
			for i := range before {
				a, b := orig[before[i]:before[i]+16], tagged[after[i]:after[i]+16]
				if !bytes.Equal(a, b) {
					t.Fatalf("指针 %d: 偏移 %d -> %d 处数据不一致", i, before[i], after[i])
				}
			}
		})
	}
}

func decodeTop(t *testing.T, data []byte) *mp4.File {
	t.Helper()
	f, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// ilstItems 返回 moov/udta/meta/ilst 中各条目 data 盒的内容。
func ilstItems(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	items := map[string][]byte{}
	var walk func(b mp4.Box, inIlst bool)
	walk = func(b mp4.Box, inIlst bool) {
		c, ok := b.(interface{ GetChildren() []mp4.Box })
		if !ok {
			return
		}
		for _, child := range c.GetChildren() {
			if inIlst {
				key, value := itemData(t, child)
				if items[key] != nil {
					t.Errorf("条目 %q 重复", key)
				}
				items[key] = value
				continue
			}
			walk(child, child.Type() == "ilst")
		}
	}
	walk(decodeTop(t, data).Moov, false)
	return items
}

// dataPointers 收集 stco/co64 与显式 base_data_offset 指向的绝对偏移。
func dataPointers(t *testing.T, data []byte) []int {
	t.Helper()
	f := decodeTop(t, data)
	var ptrs []int
	for _, trak := range f.Moov.Traks {
		stbl := trak.Mdia.Minf.Stbl
		if stbl.Stco != nil {
			for _, o := range stbl.Stco.ChunkOffset {
				ptrs = append(ptrs, int(o))
			}
		}
		if stbl.Co64 != nil {
			for _, o := range stbl.Co64.ChunkOffset {
				ptrs = append(ptrs, int(o))
			}
		}
	}
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			for _, traf := range frag.Moof.Trafs {
				if traf.Tfhd.HasBaseDataOffset() {
					ptrs = append(ptrs, int(traf.Tfhd.BaseDataOffset)+int(traf.Trun.DataOffset))
				} else {
					ptrs = append(ptrs, int(frag.Moof.StartPos)+int(traf.Trun.DataOffset))
				}
			}
		}
	}
	return ptrs
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// itemData 取 ilst 条目中 data 盒的值，freeform 条目的键为 "----:<name>"。
// mp4ff 未识别的条目按原始字节解析子盒。
func itemData(t *testing.T, item mp4.Box) (key string, value []byte) {
	t.Helper()
	var raw bytes.Buffer
	if err := item.Encode(&raw); err != nil {
		t.Fatal(err)
	}
	key = item.Type()
	body := raw.Bytes()[8:]
	for len(body) >= 8 {
		size := int(binary.BigEndian.Uint32(body))
		if size < 8 || size > len(body) {
			t.Fatalf("条目 %q 子盒大小异常", item.Type())
		}
		switch string(body[4:8]) {
		case "name":
			key += ":" + string(body[12:size]) // 跳过 version/flags(4)
		case "data":
			value = body[16:size] // 跳过类型(4)与地区(4)
		}
		body = body[size:]
	}
	return key, value
}
