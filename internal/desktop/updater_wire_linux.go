//go:build desktop && linux

package desktop

import "github.com/javinizer/javinizer-go/internal/updater"

func newBundleSwapper() updater.Swapper { return updater.NewLinuxSwapper() }
