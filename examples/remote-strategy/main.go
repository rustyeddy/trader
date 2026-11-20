// Command remote-strategy is a minimal external strategy for Trader: it listens
// on a loopback port, takes each group of bars Trader sends and always answers
// with no orders.
//
// Run it, then point a Trader backtest at the address it prints:
//
//	go run ./examples/remote-strategy -addr 127.0.0.1:9100
//
// It serves one connection and exits when Trader closes it. A message is a
// four-byte big-endian length followed by that many bytes of JSON. Trader sends
// {"bars":[...]}, the bars being marketdata.Bar as encoding/json writes it, and
// the worker answers {"orders":[]}.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/rustyeddy/trader/marketdata"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback address to listen on")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening on", ln.Addr()) // the address, for whoever started us
	conn, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}
	if err := serve(conn); err != nil {
		log.Fatal(err)
	}
}

// serve answers every group of bars with no orders until Trader closes the
// connection.
func serve(conn net.Conn) error {
	defer func() { _ = conn.Close() }()
	for groups := 1; ; groups++ {
		var in struct {
			Bars []marketdata.Bar `json:"bars"`
		}
		if err := read(conn, &in); err != nil {
			if err == io.EOF {
				fmt.Fprintf(os.Stderr, "done: %d groups\n", groups-1)
				return nil
			}
			return err
		}
		// A real strategy decides here. This one only has a look.
		if err := write(conn, map[string]any{"orders": []any{}}); err != nil {
			return err
		}
	}
}

func read(r io.Reader, v any) error {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return err
	}
	body := make([]byte, binary.BigEndian.Uint32(prefix[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func write(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	_, err = w.Write(append(buf, body...))
	return err
}
