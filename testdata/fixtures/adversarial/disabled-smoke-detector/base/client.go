package client

import "crypto/tls"

func Dial() *tls.Config {
	return &tls.Config{}
}
