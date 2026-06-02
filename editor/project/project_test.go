package project_test

import (
	"path/filepath"
	"testing"

	"github.com/hvuhsg/spliti/editor/project"
)

func TestProject_NewEmpty_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	p, err := project.NewEmptyProject(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if p.File.Name != "demo" {
		t.Fatalf("name = %q, want demo", p.File.Name)
	}

	got, err := project.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.File.Name != "demo" {
		t.Fatalf("reload name = %q, want demo", got.File.Name)
	}
	if got.File.DefaultScene != "main" {
		t.Fatalf("defaultScene = %q, want main", got.File.DefaultScene)
	}
}

func TestProject_WalksSpriteAndSceneDirs(t *testing.T) {
	dir := t.TempDir()
	p, err := project.NewEmptyProject(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	// Drop a sprite and a scene file in the project.
	sf := &project.SpriteFile{Ref: "ball", W: 1, H: 1, Rows: []string{"●"}}
	if err := project.SaveSpriteFile(filepath.Join(dir, "sprites", "ball.sprite"), sf); err != nil {
		t.Fatal(err)
	}
	scene := &project.SceneFile{Schema: 1, Name: "main"}
	if err := project.SaveSceneFile(filepath.Join(dir, "scenes", "main.scene"), scene); err != nil {
		t.Fatal(err)
	}

	got, err := project.LoadProject(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SpritePaths) != 1 {
		t.Fatalf("sprite paths = %v, want 1", got.SpritePaths)
	}
	if len(got.ScenePaths) != 1 {
		t.Fatalf("scene paths = %v, want 1", got.ScenePaths)
	}
	if got.ScenePathByName("main") == "" {
		t.Fatal("ScenePathByName(main) should resolve")
	}
	if got.SpritePathByRef("ball") == "" {
		t.Fatal("SpritePathByRef(ball) should resolve")
	}
}
