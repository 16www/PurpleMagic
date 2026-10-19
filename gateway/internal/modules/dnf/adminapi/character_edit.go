package adminapi

// The game marks a character before selection reads, waits on the account row,
// and retains the marker until character cleanup has completed.
const CharacterEditProtocol = "character-admission-v1"

type CharacterEditStatus struct {
	Protocol    string `json:"protocol"`
	AccountID   string `json:"accountId"`
	CharacterID uint16 `json:"characterId"`
	Busy        bool   `json:"busy"`
}
