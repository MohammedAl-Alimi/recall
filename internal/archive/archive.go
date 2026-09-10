package archive

import (
	"errors"

	"github.com/MohammedAl-Alimi/recall/internal/model"
)

// Archive stores the transcript of sess under RecallDir/archive/<sid>/ and
// returns the archive directory.
func Archive(p model.Paths, sess *model.Session, withSidecars bool) (string, error) {
	return "", errors.New("not implemented: archive.Archive")
}

// Restore returns a transcript path that can be resumed from.
func Restore(p model.Paths, sess *model.Session) (string, error) {
	return "", errors.New("not implemented: archive.Restore")
}

// HasArchive reports whether sid has an archived transcript and its path.
func HasArchive(p model.Paths, sid string) (string, bool) {
	return "", false
}

// Retention reads cleanupPeriodDays from settings.json.
func Retention(p model.Paths) (days int, set bool, err error) {
	return 0, false, errors.New("not implemented: archive.Retention")
}

// SetRetention writes cleanupPeriodDays into settings.json.
func SetRetention(p model.Paths, days int) error {
	return errors.New("not implemented: archive.SetRetention")
}

// MirrorHistory appends new history.jsonl lines to the recall mirror.
func MirrorHistory(p model.Paths) error {
	return errors.New("not implemented: archive.MirrorHistory")
}
