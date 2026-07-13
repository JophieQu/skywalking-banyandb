// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleKeyBracketTabsWorkWhileTyping(t *testing.T) {
	model := NewModel(Config{})
	model.activeTab = tabQuery
	model.focus = focusQuery
	model.query.SetValue("SELECT 1")

	_, handled := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if !handled {
		t.Fatal("expected ] to switch tabs while typing")
	}
	if model.activeTab != tabRun {
		t.Fatalf("expected run tab, got %v", model.activeTab)
	}
	if model.focus != focusExecution {
		t.Fatalf("expected execution focus, got %d", model.focus)
	}
}

func TestHandleKeyCtrlCloseBracketTabsWorkWhileTyping(t *testing.T) {
	model := NewModel(Config{})
	model.activeTab = tabQuery
	model.focus = focusMessage

	_, handled := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if !handled {
		t.Fatal("expected ctrl+] to switch tabs while typing")
	}
	if model.activeTab != tabRun {
		t.Fatalf("expected run tab, got %v", model.activeTab)
	}
}
