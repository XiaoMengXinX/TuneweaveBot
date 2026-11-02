package main

import (
	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

func authStatus() (any, error) {
	if client.musicU == "" {
		return AuthStatus{Authenticated: false}, nil
	}
	valid, err := client.LoginStatusValid()
	if err != nil {
		return nil, err
	}
	return AuthStatus{Authenticated: valid}, nil
}

func authBegin(mode string) (any, error) {
	if mode == "token" {
		return AuthChallenge{
			Type:    "token",
			Content: "请回复本条消息，发送你的 MUSIC_U cookie",
			Key:     "music_u",
		}, nil
	}
	key, err := client.CreateQRLoginKey()
	if err != nil {
		return nil, err
	}
	return AuthChallenge{
		Type:      "qr",
		Content:   qrLoginContent(key),
		Key:       key,
		ExpiresIn: 180,
	}, nil
}

// authSubmit 校验用户提交的 MUSIC_U，成功则返回凭据供宿主持久化。
func authSubmit(token string) (any, error) {
	client.SetMUSICU(token)
	valid, err := client.LoginStatusValid()
	if err != nil {
		return nil, err
	}
	if !valid {
		client.SetMUSICU("")
		return AuthState{State: "expired"}, nil
	}
	return AuthState{
		State:      "success",
		Credential: token,
		Persist:    &Persist{Section: "netease", Key: "music_u"},
	}, nil
}

func authPoll(key string) (any, error) {
	status, nickname, musicU, err := client.CheckQRLogin(key)
	if err != nil {
		return nil, err
	}
	switch status {
	case qrExpired:
		return AuthState{State: "expired"}, nil
	case qrWaiting:
		return AuthState{State: "waiting"}, nil
	case qrScanned:
		return AuthState{State: "scanned", Nickname: nickname}, nil
	case qrSuccess:
		client.SetMUSICU(musicU)
		return AuthState{
			State:      "success",
			Credential: musicU,
			Nickname:   nickname,
			Persist:    &Persist{Section: "netease", Key: "music_u"},
		}, nil
	}
	return AuthState{State: "waiting"}, nil
}

func authApply(c *pluginsdk.Context, credential string) error {
	client.SetMUSICU(credential)
	if credential == "" {
		return nil
	}
	if err := c.KVSet("music_u", []byte(credential)); err != nil {
		c.Log("warn", "保存 MUSIC_U 失败: "+err.Error())
	}
	return nil
}
