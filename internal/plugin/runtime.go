// Package plugin 运行 WASM 插件并提供 host 能力。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

type RuntimeConfig struct {
	CallTimeout          time.Duration
	MemoryMB             int
	HTTPMaxBytes         int64
	AllowPrivateNetworks bool
	Instances            int
}

func (c RuntimeConfig) withDefaults() RuntimeConfig {
	if c.CallTimeout <= 0 {
		c.CallTimeout = 30 * time.Second
	}
	if c.MemoryMB <= 0 {
		c.MemoryMB = 128
	}
	if c.HTTPMaxBytes <= 0 {
		c.HTTPMaxBytes = 8 << 20
	}
	if c.Instances <= 0 {
		c.Instances = 1
	}
	return c
}

type Runtime struct {
	rt         wazero.Runtime
	cfg        RuntimeConfig
	states     sync.Map // module name -> *instanceState
	store      *store.Store
	log        *logrus.Logger
	httpClient *http.Client
	seq        atomic.Uint64
}

func NewRuntime(ctx context.Context, cfg RuntimeConfig, st *store.Store, log *logrus.Logger, httpClient *http.Client) (*Runtime, error) {
	cfg = cfg.withDefaults()
	rtCfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(uint32(cfg.MemoryMB) * 16) // 1 page = 64KiB
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	r := &Runtime{
		rt:         rt,
		cfg:        cfg,
		store:      st,
		log:        log,
		httpClient: pluginHTTPClient(httpClient, cfg.AllowPrivateNetworks),
	}
	if _, err := rt.NewHostModuleBuilder("host").
		NewFunctionBuilder().
		WithFunc(r.hostCall).
		Export("call").
		Instantiate(ctx); err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	return r, nil
}

func pluginHTTPClient(base *http.Client, allowPrivate bool) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           privateNetworkDialer(allowPrivate),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
	}
	if base != nil {
		if t, ok := base.Transport.(*http.Transport); ok {
			transport.Proxy = t.Proxy
		}
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// 由插件自己的 http.Client 处理重定向
			return http.ErrUseLastResponse
		},
	}
}

func (r *Runtime) Close(ctx context.Context) error {
	return r.rt.Close(ctx)
}

type instance struct {
	module api.Module
	state  *instanceState
	name   string
}

func (i *instance) call(ctx context.Context, action string, payload, out any) error {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	input, err := json.Marshal(envelope{ABI: ABI, Action: action, Payload: payloadRaw})
	if err != nil {
		return err
	}
	alloc := i.module.ExportedFunction("alloc")
	dealloc := i.module.ExportedFunction("dealloc")
	handle := i.module.ExportedFunction("handle")
	if alloc == nil || dealloc == nil || handle == nil {
		return fmt.Errorf("插件 %s 缺少 alloc/dealloc/handle 导出", i.name)
	}
	if len(input) > 1<<24 {
		return fmt.Errorf("插件调用输入过大")
	}

	results, err := alloc.Call(ctx, uint64(len(input)))
	if err != nil {
		return fmt.Errorf("插件分配内存失败: %w", err)
	}
	if len(results) != 1 {
		return fmt.Errorf("插件 alloc 返回值异常")
	}
	inPtr := uint32(results[0])
	if !i.module.Memory().Write(inPtr, input) {
		return fmt.Errorf("写入插件内存失败")
	}
	defer func() { _, _ = dealloc.Call(context.Background(), uint64(inPtr), uint64(len(input))) }()

	results, err = handle.Call(ctx, uint64(inPtr), uint64(len(input)))
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return fmt.Errorf("插件 handle 返回值异常")
	}
	packed := results[0]
	outPtr, outLen := uint32(packed), uint32(packed>>32)
	data, ok := i.module.Memory().Read(outPtr, outLen)
	if !ok {
		return fmt.Errorf("读取插件输出失败")
	}
	raw := append([]byte(nil), data...)
	_, _ = dealloc.Call(context.Background(), uint64(outPtr), uint64(outLen))

	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("解析插件响应失败: %w", err)
	}
	if !resp.OK {
		if resp.Error == nil {
			return &pluginError{Code: "internal", Message: "插件未返回错误详情"}
		}
		return resp.Error
	}
	if out != nil && len(resp.Data) != 0 {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			// schema 不匹配是数据错误而非实例崩溃，包成 pluginError 让上层保留实例
			return &pluginError{Code: "invalid_response", Message: err.Error()}
		}
	}
	return nil
}

type Plugin struct {
	runtime      *Runtime
	manifest     Manifest
	compiled     wazero.CompiledModule
	config       json.RawMessage
	allowedHosts []string
	pool         chan *instance
	closed       atomic.Bool

	// credential 在扫码登录后会更新，重建实例时须用最新值，否则会退回旧 cookie
	credMu     sync.Mutex
	credential string
}

func (p *Plugin) cred() string {
	p.credMu.Lock()
	defer p.credMu.Unlock()
	return p.credential
}

func (p *Plugin) setCred(credential string) {
	p.credMu.Lock()
	p.credential = credential
	p.credMu.Unlock()
}

func (r *Runtime) loadPlugin(ctx context.Context, wasmPath string, config json.RawMessage, credential string) (*Plugin, error) {
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	compiled, err := r.rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("编译插件 %s 失败: %w", wasmPath, err)
	}
	p := &Plugin{runtime: r, compiled: compiled, config: config, credential: credential}

	// 探测阶段只读 manifest：禁止 HTTP（空白名单）并加超时，防止插件卡住启动
	probeCtx, cancelProbe := context.WithTimeout(ctx, r.cfg.CallTimeout)
	defer cancelProbe()
	probe, err := r.instantiate(probeCtx, p, pluginNameHint(wasmPath), []string{})
	if err != nil {
		_ = compiled.Close(ctx)
		return nil, err
	}
	var manifest Manifest
	if err := probe.call(probeCtx, "manifest", struct{}{}, &manifest); err != nil {
		_ = probe.module.Close(ctx)
		r.states.Delete(probe.name)
		_ = compiled.Close(ctx)
		return nil, fmt.Errorf("读取插件 manifest 失败: %w", err)
	}
	if err := manifest.validate(); err != nil {
		_ = probe.module.Close(ctx)
		r.states.Delete(probe.name)
		_ = compiled.Close(ctx)
		return nil, err
	}
	p.manifest = manifest
	p.allowedHosts = manifest.HTTPAllowedHosts
	probe.state.plugin = manifest.Name
	probe.state.allowedHosts = p.allowedHosts

	if err := r.initInstance(ctx, probe, credential); err != nil {
		_ = probe.module.Close(ctx)
		r.states.Delete(probe.name)
		_ = compiled.Close(ctx)
		return nil, err
	}

	count := instanceCount(r.cfg.Instances, manifest.Resources.Instances)
	p.pool = make(chan *instance, count)
	p.pool <- probe
	for i := 1; i < count; i++ {
		inst, err := r.instantiate(ctx, p, p.manifest.Name, p.allowedHosts)
		if err != nil {
			p.Close(ctx)
			return nil, err
		}
		if err := r.initInstance(ctx, inst, credential); err != nil {
			_ = inst.module.Close(ctx)
			r.states.Delete(inst.name)
			p.Close(ctx)
			return nil, err
		}
		p.pool <- inst
	}
	return p, nil
}

// instanceCount 以运维配置为准，manifest 的声明只作上限建议，并设硬上限防止耗尽内存。
func instanceCount(configured, manifestWant int) int {
	const hardMax = 16
	count := configured
	if manifestWant > 0 && manifestWant < count {
		count = manifestWant
	}
	if count < 1 {
		count = 1
	}
	if count > hardMax {
		count = hardMax
	}
	return count
}

func (r *Runtime) instantiate(ctx context.Context, p *Plugin, pluginName string, hosts []string) (*instance, error) {
	name := fmt.Sprintf("plugin/%s/%d", pluginName, r.seq.Add(1))
	state := &instanceState{
		plugin:       pluginName,
		config:       p.config,
		kv:           r.store,
		allowedHosts: hosts,
		maxHTTPBytes: r.cfg.HTTPMaxBytes,
		httpClient:   r.httpClient,
		log:          r.log,
	}
	r.states.Store(name, state)
	module, err := r.rt.InstantiateModule(ctx, p.compiled, wazero.NewModuleConfig().
		WithName(name).
		WithStartFunctions("_initialize"))
	if err != nil {
		r.states.Delete(name)
		return nil, fmt.Errorf("实例化插件 %s 失败: %w", pluginName, err)
	}
	return &instance{module: module, state: state, name: name}, nil
}

func (r *Runtime) initInstance(ctx context.Context, inst *instance, credential string) error {
	config := inst.state.config
	payload := map[string]any{"config": json.RawMessage(config), "credential": credential}
	if len(config) == 0 {
		payload["config"] = map[string]any{}
	}
	callCtx, cancel := context.WithTimeout(ctx, r.cfg.CallTimeout)
	defer cancel()
	if err := inst.call(callCtx, "init", payload, nil); err != nil {
		return fmt.Errorf("初始化插件 %s 失败: %w", inst.state.plugin, err)
	}
	return nil
}

func (p *Plugin) acquire(ctx context.Context) (*instance, error) {
	select {
	case inst := <-p.pool:
		return inst, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Plugin) release(inst *instance) {
	if p.closed.Load() {
		_ = inst.module.Close(context.Background())
		p.runtime.states.Delete(inst.name)
		return
	}
	select {
	case p.pool <- inst:
	default:
		_ = inst.module.Close(context.Background())
		p.runtime.states.Delete(inst.name)
	}
}

// call 执行一次插件动作，超时/崩溃的实例会被替换。
func (p *Plugin) call(ctx context.Context, action string, payload, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, p.runtime.cfg.CallTimeout)
	defer cancel()

	inst, err := p.acquire(callCtx)
	if err != nil {
		return err
	}
	err = inst.call(callCtx, action, payload, out)
	if err == nil {
		p.release(inst)
		return nil
	}

	if pe, ok := errors.AsType[*pluginError](err); ok {
		p.release(inst)
		return mapError(pe)
	}
	// 超时、trap 或内存错误：丢弃实例并补一个
	_ = inst.module.Close(context.Background())
	p.runtime.states.Delete(inst.name)
	p.replace(ctx)
	return err
}

// broadcastCredential 执行 auth.apply 并记录最新凭据，供重建实例复用。
func (p *Plugin) broadcastCredential(ctx context.Context, credential string) error {
	err := p.broadcast(ctx, "auth.apply", map[string]string{"credential": credential})
	// 即便部分实例失败，也记录新凭据：重建实例会用它，优于退回加载时的旧值
	p.setCred(credential)
	return err
}

// broadcast 对池内每个实例各执行一次动作（如 auth.apply），避免多实例凭据不一致。
func (p *Plugin) broadcast(ctx context.Context, action string, payload any) error {
	if p.closed.Load() {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, p.runtime.cfg.CallTimeout)
	defer cancel()

	count := len(p.pool)
	var firstErr error
	for range count {
		var inst *instance
		select {
		case inst = <-p.pool:
		case <-callCtx.Done():
			return callCtx.Err()
		}
		err := inst.call(callCtx, action, payload, nil)
		if err == nil {
			p.release(inst)
			continue
		}
		// 业务错误不代表实例损坏，释放回池即可；仅 trap/超时/内存错误才丢弃
		if pe, ok := errors.AsType[*pluginError](err); ok {
			p.release(inst)
			if firstErr == nil {
				firstErr = mapError(pe)
			}
			continue
		}
		_ = inst.module.Close(context.Background())
		p.runtime.states.Delete(inst.name)
		p.replace(ctx)
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *Plugin) replace(ctx context.Context) {
	if p.closed.Load() {
		return
	}
	// 用不随调用方取消的 ctx 重建，否则调用被取消后实例池会越缩越小直至挂死
	ctx = context.WithoutCancel(ctx)
	ctx, cancel := context.WithTimeout(ctx, p.runtime.cfg.CallTimeout)
	defer cancel()
	inst, err := p.runtime.instantiate(ctx, p, p.manifest.Name, p.allowedHosts)
	if err != nil {
		p.runtime.log.WithField("plugin", p.manifest.Name).Errorf("重建插件实例失败: %v", err)
		return
	}
	if err := p.runtime.initInstance(ctx, inst, p.cred()); err != nil {
		_ = inst.module.Close(ctx)
		p.runtime.states.Delete(inst.name)
		p.runtime.log.WithField("plugin", p.manifest.Name).Errorf("初始化重建实例失败: %v", err)
		return
	}
	p.release(inst)
}

var hintRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// pluginNameHint 用文件名作为 manifest 读取前的临时命名空间。
func pluginNameHint(path string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	name = hintRe.ReplaceAllString(name, "")
	if !nameRe.MatchString(name) {
		name = "probe"
	}
	return name
}

func (p *Plugin) Close(ctx context.Context) {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}
	for {
		select {
		case inst := <-p.pool:
			_ = inst.module.Close(ctx)
			p.runtime.states.Delete(inst.name)
		default:
			_ = p.compiled.Close(ctx)
			return
		}
	}
}
