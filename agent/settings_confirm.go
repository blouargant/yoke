package agent

import (
	"context"

	"github.com/blouargant/omnis/internal/askuser"
	"github.com/blouargant/omnis/internal/settings"
)

// newSettingsConfirmer builds the process-wide confirmer the settings tool group
// (mounted on the Helper agent) uses to gate security-sensitive changes —
// permission rules, lifecycle hooks, and provider credentials. It asks the user
// in the active session via the ask-user widget; routine changes never reach it.
// Returns nil when there is no ask-user registry, which disables sensitive
// changes (settings.confirmSensitive then declines rather than applying — the
// same off-surface safety the skill dependency gate has).
func newSettingsConfirmer(reg *askuser.Registry) settings.Confirmer {
	if reg == nil {
		return nil
	}
	return &settingsConfirmer{reg: reg}
}

type settingsConfirmer struct{ reg *askuser.Registry }

const (
	settingsChoiceCancel = "Cancel"
	settingsChoiceApply  = "Apply"
)

func (s *settingsConfirmer) Confirm(ctx context.Context, sessionID, summary string) (bool, error) {
	if s == nil || s.reg == nil || sessionID == "" {
		return false, nil
	}
	q := askuser.Question{
		Kind:    askuser.KindSingle,
		Prompt:  "**Apply this settings change?**\n\n" + summary,
		Choices: []string{settingsChoiceCancel, settingsChoiceApply},
		Default: settingsChoiceCancel,
		Item:    &askuser.QuestionItem{Kind: "settings", Name: "settings change"},
	}
	ans, err := s.reg.Ask(ctx, sessionID, q)
	if err != nil || ans.Cancelled || len(ans.Selected) == 0 {
		return false, err
	}
	return ans.Selected[0] == settingsChoiceApply, nil
}
