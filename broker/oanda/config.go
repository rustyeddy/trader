package oanda

import "os"

type Config struct {
	Token   string
	baseURL string
}

var (
	config Config
)

func init() {
	config.Token = os.Getenv("OANDA_TOKEN")
}
