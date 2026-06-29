package memory_test

import (
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
	"github.com/tas50/cinc-zero/internal/store/backendtest"
	"github.com/tas50/cinc-zero/internal/store/memory"
)

func TestConformance(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) store.Backend { return memory.New() })
}
