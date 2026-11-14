package projectmove

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type moveRecord struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	VolumeUUID  string `json:"volume_uuid"`
	Digest      string `json:"digest"`
}

func recordPath(source, uuid string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(source + "\x00" + strings.ToUpper(uuid)))
	return filepath.Join(home, ".local", "state", "apfsctl", "moves", hex.EncodeToString(key[:])+".json"), nil
}
func readRecord(source, uuid string) (*moveRecord, error) {
	path, err := recordPath(source, uuid)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r moveRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("invalid move record: %w", err)
	}
	if r.Source != source || !strings.EqualFold(r.VolumeUUID, uuid) || r.Digest == "" || r.Destination == "" {
		return nil, errors.New("move record identity conflict")
	}
	return &r, nil
}
func writeRecord(r moveRecord) error {
	path, err := recordPath(r.Source, r.VolumeUUID)
	if err != nil {
		return err
	}
	old, err := readRecord(r.Source, r.VolumeUUID)
	if err != nil {
		return err
	}
	if old != nil {
		if *old == r {
			return nil
		}
		return errors.New("move record conflicts with previous completed transfer")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".move-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func digest(entries []entry) (string, error) {
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
