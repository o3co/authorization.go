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

package requesttracking

import (
	"regexp"
	"testing"
)

// Verify that generateRequestID conforms to the "YYYYMMDDHHmmss_<hex>" format.
func TestGenerateRequestID_Format(t *testing.T) {
	id := generateRequestID()
	pattern := regexp.MustCompile(`^\d{14}_[0-9a-f]+$`)
	if !pattern.MatchString(id) {
		t.Errorf("id %q does not match expected pattern YYYYMMDDHHmmss_<hex>", id)
	}
}

// Verify that 100 consecutively generated IDs are all unique.
func TestGenerateRequestID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		id := generateRequestID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate request ID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}
