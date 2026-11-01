package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	ncm "github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

var recognizer = &ncm.Client{HTTP: pluginsdk.NewHTTPClient()}

func recognize(_ *pluginsdk.Context, audio []byte) (any, error) {
	response, err := recognizer.Recognize(context.Background(), audio)
	if err != nil {
		return nil, err
	}
	if response.Code != http.StatusOK {
		return nil, pluginsdk.Fail("internal", fmt.Sprintf("识曲接口返回错误码 %d", response.Code))
	}
	var data struct {
		Result []struct {
			Song struct {
				ID int `json:"id"`
			} `json:"song"`
		} `json:"result"`
	}
	if len(response.Data) != 0 {
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return nil, err
		}
	}
	if len(data.Result) == 0 || data.Result[0].Song.ID <= 0 {
		return nil, pluginsdk.Fail("no_result", "识别失败，可能是录音时间太短")
	}
	return map[string]any{"ref": songRef(data.Result[0].Song.ID)}, nil
}
