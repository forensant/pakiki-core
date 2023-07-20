package project

import (
	"os"

	"github.com/forensant/pakiki-core/internal/request_queue"
	"github.com/zalando/go-keyring"
)

func init() {
	if ioHub == nil {
		keyring.MockInit()
		request_queue.Init()

		pf, _ := os.CreateTemp("", "pakiki-testprojdb-*")
		tf, _ := os.CreateTemp("", "pakiki-testtmpdb-*")
		ioHub := NewIOHub("", "")
		ioHub.Run(pf.Name(), tf.Name())
	}
}
