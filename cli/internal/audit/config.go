package audit

import "github.com/bravros/private/internal/config"

// Re-export for use within the audit package.
// All new code should import internal/config directly.
type BravrosConfig = config.BravrosConfig

var LoadBravrosConfig = config.LoadBravrosConfig
