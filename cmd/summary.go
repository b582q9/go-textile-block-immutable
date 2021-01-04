package cmd

import (
	"net/http"

	"github.com/b582q9/go-textile-sapien/pb"
)

func Summary() error {
	var info pb.Summary
	res, err := executeJsonPbCmd(http.MethodGet, "summary", params{}, &info)
	if err != nil {
		return err
	}
	output(res)
	return nil
}
