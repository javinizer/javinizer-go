package temp

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
)

func TestMain(m *testing.M) {
	ssrf.AllowHostForTest("127.0.0.1")
	os.Exit(m.Run())
}
