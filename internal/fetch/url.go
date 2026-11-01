package fetch

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
)

// ValidateURL 校验插件返回的下载地址：只允许 http/https，且（默认）不指向
// 环回、私网、链路本地或未指定地址，避免插件把宿主当作 SSRF 跳板。
func ValidateURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("下载地址无效: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("不支持的下载协议 %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("下载地址缺少主机名")
	}
	if allowPrivate {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if isPrivateAddr(ip.Unmap()) {
			return fmt.Errorf("拒绝下载私网地址 %s", host)
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	if err != nil {
		return fmt.Errorf("解析下载地址失败: %w", err)
	}
	for _, ip := range ips {
		if isPrivateAddr(ip.Unmap()) {
			return fmt.Errorf("拒绝下载私网地址 %s (%s)", host, ip)
		}
	}
	return nil
}

func isPrivateAddr(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
