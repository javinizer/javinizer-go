package core

import "sync"

// ConfigUpdateMutex serializes config updates.
var ConfigUpdateMutex sync.Mutex
