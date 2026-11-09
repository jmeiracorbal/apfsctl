package main

import (
	"strings"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

func TestSanitizeVolumeName(t *testing.T) {
	got, err := sanitizeVolumeName("  Proyectos / prueba: 1  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Proyectos-prueba-1" {
		t.Fatalf("unexpected sanitized volume name %q", got)
	}
	if _, err := sanitizeVolumeName("../:--"); err == nil {
		t.Fatal("name without letters or numbers must fail")
	}
}

func TestActiveSystemContainerUsesMountedDataVolume(t *testing.T) {
	snapshot := domain.Snapshot{
		Containers: []domain.Container{
			{UUID: "OTHER", Reference: "disk8"},
			{UUID: "SYSTEM", Reference: "disk3"},
		},
		Volumes: []domain.Volume{
			{Name: "Backup", UUID: "V1", ContainerUUID: "OTHER", Device: "disk8s1", Roles: []string{"Data"}, MountPoint: "/Volumes/Backup"},
			{Name: "Data", UUID: "V2", ContainerUUID: "SYSTEM", Device: "disk3s5", Roles: []string{"Data"}, MountPoint: "/System/Volumes/Data"},
		},
	}
	got, err := activeSystemContainer(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got.UUID != "SYSTEM" || got.Reference != "disk3" {
		t.Fatalf("selected wrong system container: %#v", got)
	}
}

func TestActiveSystemContainerFailsClosedWhenAmbiguous(t *testing.T) {
	snapshot := domain.Snapshot{
		Volumes: []domain.Volume{
			{Name: "Data1", UUID: "V1", ContainerUUID: "ONE", Device: "disk3s1", Roles: []string{"Data"}, MountPoint: "/System/Volumes/Data"},
			{Name: "Data2", UUID: "V2", ContainerUUID: "TWO", Device: "disk4s1", Roles: []string{"Data"}, MountPoint: "/System/Volumes/Data"},
		},
	}
	_, err := activeSystemContainer(snapshot)
	if err == nil || !strings.Contains(err.Error(), "refusing to guess") {
		t.Fatalf("expected ambiguous system container to fail safely, got %v", err)
	}
}
