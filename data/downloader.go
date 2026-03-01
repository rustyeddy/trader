package data

import (
	"context"
	"net/http"
)

type downloader struct {
	*http.Client
}

func (dl *downloader) download(ctx context.Context, df *datafile) (err error) {
	err = df.download(ctx, dl.Client)
	return err
}
