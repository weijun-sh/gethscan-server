package main

import (
	"fmt"

	"github.com/weijun-sh/gethscan-server/cmd/utils"
	"github.com/weijun-sh/gethscan-server/common"
	"github.com/weijun-sh/gethscan-server/log"
	"github.com/urfave/cli/v2"
)

var (
	setnonceCommand = &cli.Command{
		Action:    setnonce,
		Name:      "setnonce",
		Usage:     "admin swap nonce",
		ArgsUsage: "<swapin|swapout> <nonce> <pairID>",
		Description: `
admin swap nonce,
swapin nonce is on destination blockchain,
swapout nonce is on source blockchain.
`,
		Flags: commonAdminFlags,
	}
)

func setnonce(ctx *cli.Context) error {
	utils.SetLogger(ctx)
	method := "setnonce"
	if ctx.NArg() != 3 {
		_ = cli.ShowCommandHelp(ctx, method)
		fmt.Println()
		return fmt.Errorf("invalid arguments: %q", ctx.Args())
	}

	err := prepare(ctx)
	if err != nil {
		return err
	}

	operation := ctx.Args().Get(0)
	nonce := ctx.Args().Get(1)
	pairID := ctx.Args().Get(2)

	_, err = common.GetUint64FromStr(nonce)
	if err != nil {
		return fmt.Errorf("wrong nonce value '%v'", nonce)
	}

	switch operation {
	case swapinOp, swapoutOp:
	default:
		return fmt.Errorf("unknown operation '%v'", operation)
	}

	log.Printf("admin setnonce: %v %v %v", operation, nonce, pairID)

	params := []string{operation, nonce, pairID}
	result, err := adminCall(method, params)

	log.Printf("result is '%v'", result)
	return err
}
