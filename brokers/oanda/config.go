package oanda

import (
	"os"
)

type Config struct {
	Token string
}

var (
	config Config
)

func init() {
	config.Token = os.Getenv("OANDA_TOKEN")
}
