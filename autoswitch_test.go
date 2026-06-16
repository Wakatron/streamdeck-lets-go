package main

import (
	"encoding/json"
	"testing"

	"streamdeck-lets-go/internal/config"
)

func TestFindFocusedSwayNode(t *testing.T) {
	input := `{
		"id": 0,
		"type": "root",
		"nodes": [
			{
				"type": "output",
				"name": "eDP-1",
				"nodes": [
					{
						"type": "workspace",
						"name": "1",
						"nodes": [
							{
								"type": "con",
								"focused": false,
								"app_id": "firefox",
								"name": "Firefox",
								"nodes": []
							}
						]
					},
					{
						"type": "workspace",
						"name": "2",
						"nodes": [
							{
								"type": "con",
								"focused": true,
								"app_id": "Alacritty",
								"name": "Alacritty",
								"nodes": []
							}
						]
					}
				],
				"floating_nodes": []
			}
		]
	}`

	var root swayNode
	if err := json.Unmarshal([]byte(input), &root); err != nil {
		t.Fatal(err)
	}

	node := findFocusedSwayNode(&root)
	if node == nil {
		t.Fatal("expected focused node, got nil")
	}
	if node.AppID == nil || *node.AppID != "Alacritty" {
		t.Fatalf("expected Alacritty, got %v", node.AppID)
	}
	if node.Name != "Alacritty" {
		t.Fatalf("expected name Alacritty, got %s", node.Name)
	}
}

func TestFindFocusedSwayNode_XWayland(t *testing.T) {
	input := `{
		"id": 0,
		"type": "root",
		"nodes": [
			{
				"type": "output",
				"nodes": [
					{
						"type": "workspace",
						"nodes": [
							{
								"type": "con",
								"focused": true,
								"app_id": null,
								"name": "Steam",
								"window_properties": {"class": "steam"},
								"nodes": []
							}
						]
					}
				]
			}
		]
	}`

	var root swayNode
	json.Unmarshal([]byte(input), &root)

	node := findFocusedSwayNode(&root)
	if node == nil {
		t.Fatal("expected focused node, got nil")
	}
	if node.Props == nil || node.Props.Class != "steam" {
		t.Fatal("expected window_properties.class = steam")
	}
}

func TestFindFocusedSwayNode_NoFocus(t *testing.T) {
	input := `{
		"id": 0,
		"type": "root",
		"nodes": [
			{
				"type": "output",
				"nodes": [
					{
						"type": "workspace",
						"nodes": [
							{
								"type": "con",
								"focused": false,
								"app_id": "kitty",
								"name": "kitty",
								"nodes": []
							}
						]
					}
				]
			}
		]
	}`

	var root swayNode
	json.Unmarshal([]byte(input), &root)
	node := findFocusedSwayNode(&root)
	if node != nil {
		t.Fatal("expected nil when no focused node")
	}
}

func TestAutoSwitchManager_Evaluate(t *testing.T) {
	rules := []config.SwitchRule{
		{WMClass: "firefox", Page: "browser"},
		{WMClass: "Alacritty|kitty", Title: ".*vim.*", Page: "coding", Stay: true},
	}

	m := NewAutoSwitchManager(rules, "default")

	page, ok := m.Evaluate(Window{WMClass: "firefox", Title: "Mozilla Firefox"}, "default")
	if !ok || page != "browser" {
		t.Fatalf("expected browser, got %s/%v", page, ok)
	}

	page, ok = m.Evaluate(Window{WMClass: "Alacritty", Title: "nvim main.go"}, "default")
	if !ok || page != "coding" {
		t.Fatalf("expected coding, got %s/%v", page, ok)
	}

	page, ok = m.Evaluate(Window{WMClass: "firefox", Title: "YouTube"}, "browser")
	if !ok || page != "browser" {
		t.Fatalf("expected browser again, got %s/%v", page, ok)
	}
}

func TestAutoSwitchManager_Stay(t *testing.T) {
	rules := []config.SwitchRule{
		{WMClass: "firefox", Page: "browser", Stay: false},
	}

	m := NewAutoSwitchManager(rules, "home")

	m.Evaluate(Window{WMClass: "firefox", Title: "Mozilla Firefox"}, "home")

	page, ok := m.Evaluate(Window{WMClass: "thunar", Title: "Files"}, "browser")
	if !ok || page != "home" {
		t.Fatalf("expected revert to home, got %s/%v", page, ok)
	}
}

func TestAutoSwitchManager_StayTrue(t *testing.T) {
	rules := []config.SwitchRule{
		{WMClass: "firefox", Page: "browser", Stay: true},
	}

	m := NewAutoSwitchManager(rules, "home")

	m.Evaluate(Window{WMClass: "firefox", Title: "Mozilla Firefox"}, "home")

	page, ok := m.Evaluate(Window{WMClass: "thunar", Title: "Files"}, "browser")
	if ok {
		t.Fatalf("expected no switch (stay=true), got %s", page)
	}
}

func TestAutoSwitchManager_ManualOverride(t *testing.T) {
	rules := []config.SwitchRule{
		{WMClass: "firefox", Page: "browser", Stay: false},
	}

	m := NewAutoSwitchManager(rules, "home")

	// 1. Firefox opens → auto-switch to "browser"
	m.Evaluate(Window{WMClass: "firefox", Title: "Mozilla Firefox"}, "home")

	// 2. User manually switches to "settings"
	m.NotifyManualPage("settings")

	// 3. Window changes to thunar on "settings" → no match → stay on settings
	page, ok := m.Evaluate(Window{WMClass: "thunar", Title: "Files"}, "settings")
	if ok {
		t.Fatalf("expected no switch (already on manual page), got %s", page)
	}

	// 4. Firefox comes back → auto-switch to "browser" again
	page, ok = m.Evaluate(Window{WMClass: "firefox", Title: "Mozilla Firefox"}, "settings")
	if !ok || page != "browser" {
		t.Fatalf("expected browser, got %s/%v", page, ok)
	}

	// 5. Thunar again, current page is "browser" (auto) → no match → revert to "settings"
	page, ok = m.Evaluate(Window{WMClass: "thunar", Title: "Files"}, "browser")
	if !ok || page != "settings" {
		t.Fatalf("expected revert to settings, got %s/%v", page, ok)
	}
}
