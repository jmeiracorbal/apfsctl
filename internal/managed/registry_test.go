package managed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
)

func TestAvailableChecksIdentityAndUsesLiveMountPath(t *testing.T) {
	records := []Record{{Name:"Repos",VolumeUUID:"V1",ContainerUUID:"C1"}}
	s := domain.Snapshot{
		Containers: []domain.Container{{UUID:"C1",Reference:"disk3"}},
		Volumes: []domain.Volume{{Name:"Repos",UUID:"V1",ContainerUUID:"C1",Device:"disk3s2",Encrypted:true,MountPoint:"/disco-cero-uno"}},
	}
	got, err := Available(records,s)
	if err != nil || len(got)!=1 || got[0].MountPoint!="/disco-cero-uno" {
		t.Fatalf("unexpected volume: %+v, %v",got,err)
	}
	s.Volumes[0].Locked=true
	s.Volumes[0].MountPoint=""
	got,err=Available(records,s)
	if err!=nil || len(got)!=0 {t.Fatalf("locked volume should not complete: %+v, %v",got,err)}
	s.Volumes[0].Locked=false
	s.Volumes[0].Name="Other"
	if _,err:=Available(records,s); err==nil {t.Fatal("changed UUID identity must be rejected")}
}
func TestRegistryWriteIsIdempotent(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"apfsctl","volumes.json")
	store:=Store{Path:path}
	record:=[]Record{{Name:"Repos",VolumeUUID:"V1",ContainerUUID:"C1"}}
	if err:=store.Write(record);err!=nil{t.Fatal(err)}
	info,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
	if err:=store.Write(record);err!=nil{t.Fatal(err)}
	info2,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
	if !info.ModTime().Equal(info2.ModTime()) {t.Fatal("repeated write should preserve existing file")}
	got,err:=store.Read()
	if err!=nil || len(got)!=1 || got[0].Name!="Repos" {t.Fatalf("registry: %+v %v",got,err)}
}
