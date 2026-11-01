package plugin

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

type envelope struct {
	ABI     int             `json:"abi"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *pluginError    `json:"error,omitempty"`
}

type pluginError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *pluginError) Error() string { return e.Code + ": " + e.Message }

// mapError 把插件错误码映射为宿主哨兵错误，便于 handler 统一处理。
func mapError(err error) error {
	var pe *pluginError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case "no_result":
		return fmt.Errorf("%w: %s", provider.ErrNoResult, pe.Message)
	case "not_found":
		return fmt.Errorf("%w: %s", provider.ErrNotFound, pe.Message)
	case "unsupported":
		return fmt.Errorf("%w: %s", provider.ErrUnsupported, pe.Message)
	case "auth_required":
		return fmt.Errorf("%w: %s", provider.ErrAuthRequired, pe.Message)
	case "rate_limited":
		return fmt.Errorf("%w: %s", provider.ErrRateLimited, pe.Message)
	case "invalid_request":
		return fmt.Errorf("插件请求无效: %s", pe.Message)
	default:
		return fmt.Errorf("插件错误: %s", pe.Message)
	}
}
