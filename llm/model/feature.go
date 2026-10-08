package model

type FeatureFlag int

const (
	CHAT FeatureFlag = iota
	LISTEN
	DICTATE
	MEMORY
	SYSTEM_MEMORY
	TOOL
	NAN
)

func FeatureFromInt(num int) FeatureFlag {
	switch num {
	case 0:
		return CHAT
	case 1:
		return LISTEN
	case 2:
		return DICTATE
	case 3:
		return MEMORY
	case 4:
		return SYSTEM_MEMORY
	case 5:
		return TOOL
	default:
		return NAN
	}
}
