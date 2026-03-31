// Copyright 2026 1o1 Co. Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tokenintrospection

import (
	"testing"
	"time"
)

// Verify basic Set and Get.
func TestInMemoryCache_SetGet(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Minute)
	result := &IntrospectionResult{Subject: "user-1"}

	cache.Set("key1", result)

	got, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
}

// Verify that expired entries return a miss.
func TestInMemoryCache_Expiry(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Millisecond)
	cache.Set("key1", &IntrospectionResult{Subject: "user-1"})

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

// Verify that Get returns miss for unknown keys.
func TestInMemoryCache_Miss(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Minute)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

// Verify that ExpiresAt shorter than TTL bounds the cache entry lifetime.
func TestInMemoryCache_ExpiresAtBoundsEntry(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Minute)
	cache.Set("key1", &IntrospectionResult{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(1 * time.Millisecond),
	})

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected cache miss after token ExpiresAt")
	}
}

// Verify that keys with different scheme prefixes don't collide.
func TestInMemoryCache_SharedAcrossSchemes(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Minute)

	cache.Set("bearer:token-abc", &IntrospectionResult{Subject: "bearer-user"})
	cache.Set("basic:token-abc", &IntrospectionResult{Subject: "basic-user"})

	got1, ok := cache.Get("bearer:token-abc")
	if !ok || got1.Subject != "bearer-user" {
		t.Errorf("bearer entry = %v, want bearer-user", got1)
	}

	got2, ok := cache.Get("basic:token-abc")
	if !ok || got2.Subject != "basic-user" {
		t.Errorf("basic entry = %v, want basic-user", got2)
	}
}
