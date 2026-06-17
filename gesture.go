package main

import (
	"sync"
	"time"

	"streamdeck-lets-go/internal/config"
)

type ActionCallback func(idx int, a *config.Action)

type gestureKeyState struct {
	mu         sync.Mutex
	pressedAt  time.Time
	lastTapAt  time.Time
	holdTimer  *time.Timer
	holdActive bool
	pendingTap *time.Timer
}

type GestureEngine struct {
	mu       sync.Mutex
	states   map[int]*gestureKeyState
	longMs   time.Duration
	doubleMs time.Duration
	onAction ActionCallback
}

func NewGestureEngine(longMs, doubleMs int, cb ActionCallback) *GestureEngine {
	if longMs <= 0 {
		longMs = config.DefaultLongPressMs
	}
	if doubleMs <= 0 {
		doubleMs = config.DefaultDoubleTapMs
	}
	return &GestureEngine{
		states:   make(map[int]*gestureKeyState),
		longMs:   time.Duration(longMs) * time.Millisecond,
		doubleMs: time.Duration(doubleMs) * time.Millisecond,
		onAction: cb,
	}
}

func (ge *GestureEngine) ReloadTiming(longMs, doubleMs int) {
	ge.mu.Lock()
	defer ge.mu.Unlock()
	if longMs > 0 {
		ge.longMs = time.Duration(longMs) * time.Millisecond
	}
	if doubleMs > 0 {
		ge.doubleMs = time.Duration(doubleMs) * time.Millisecond
	}
}

func (ge *GestureEngine) Reset() {
	ge.mu.Lock()
	defer ge.mu.Unlock()
	for _, s := range ge.states {
		s.mu.Lock()
		s.cancelTimers()
		s.holdActive = false
		s.lastTapAt = time.Time{}
		s.mu.Unlock()
	}
}

func (s *gestureKeyState) cancelTimers() {
	if s.holdTimer != nil {
		s.holdTimer.Stop()
		s.holdTimer = nil
	}
	if s.pendingTap != nil {
		s.pendingTap.Stop()
		s.pendingTap = nil
	}
}

func (ge *GestureEngine) getState(index int) *gestureKeyState {
	ge.mu.Lock()
	defer ge.mu.Unlock()
	s, ok := ge.states[index]
	if !ok {
		s = &gestureKeyState{}
		ge.states[index] = s
	}
	return s
}

func findAction(actions []config.KeyAction, trigger string) *config.Action {
	for _, a := range actions {
		if a.Trigger == trigger {
			return &a.Action
		}
	}
	return nil
}

func (ge *GestureEngine) HandleEvent(evt Event, actions []config.KeyAction) {
	state := ge.getState(evt.Index)

	if len(actions) == 0 {
		return
	}

	switch evt.Kind {
	case EventKeyPressed:
		ge.handleKeyPress(state, evt, actions)
	case EventKeyReleased:
		ge.handleKeyRelease(state, evt, actions)
	}
}

func (ge *GestureEngine) handleKeyPress(state *gestureKeyState, evt Event, actions []config.KeyAction) {
	state.mu.Lock()
	state.cancelTimers()
	state.holdActive = false
	state.pressedAt = evt.At
	state.holdTimer = time.AfterFunc(ge.longMs, func() {
		state.mu.Lock()
		state.holdActive = true
		state.holdTimer = nil
		state.mu.Unlock()

		if a := findAction(actions, "hold_start"); a != nil {
			ge.onAction(evt.Index, a)
		} else if a := findAction(actions, "long_press"); a != nil {
			ge.onAction(evt.Index, a)
		}
	})
	state.mu.Unlock()
}

func (ge *GestureEngine) handleKeyRelease(state *gestureKeyState, evt Event, actions []config.KeyAction) {
	state.mu.Lock()
	state.cancelTimers()

	if state.holdActive {
		state.holdActive = false
		state.mu.Unlock()
		if a := findAction(actions, "hold_end"); a != nil {
			ge.onAction(evt.Index, a)
		}
		return
	}

	if !state.lastTapAt.IsZero() && evt.At.Sub(state.lastTapAt) <= ge.doubleMs {
		state.lastTapAt = time.Time{}
		state.mu.Unlock()
		if a := findAction(actions, "double_tap"); a != nil {
			ge.onAction(evt.Index, a)
		}
		return
	}

	lastTap := evt.At
	state.lastTapAt = lastTap

	state.pendingTap = time.AfterFunc(ge.doubleMs, func() {
		state.mu.Lock()
		if state.lastTapAt.Equal(lastTap) {
			state.lastTapAt = time.Time{}
			state.pendingTap = nil
			state.mu.Unlock()
			if a := findAction(actions, "tap"); a != nil {
				ge.onAction(evt.Index, a)
			}
			return
		}
		state.mu.Unlock()
	})
	state.mu.Unlock()
}
