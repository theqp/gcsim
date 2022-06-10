package hunter

import (
	"github.com/genshinsim/gcsim/internal/weapons/common"
	"github.com/genshinsim/gcsim/pkg/core"
	"github.com/genshinsim/gcsim/pkg/core/keys"
)

func init() {
	core.RegisterWeaponFunc(keys.HuntersBow, common.NewNoEffect)
	core.RegisterWeaponFunc(keys.SeasonedHuntersBow, common.NewNoEffect)
}
