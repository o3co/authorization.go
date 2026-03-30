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
	"crypto/rand"
	"fmt"
	"time"
)

// generateRequestID generates a unique request ID.
// Format: YYYYMMDDHHmmss_<uuid-v4-no-dashes>
func generateRequestID() string {
	now := time.Now().UTC()
	timestamp := now.Format("20060102150405")

	var b [16]byte
	// crypto/rand.Read always returns len(b) and nil error on supported platforms (Go 1.20+).
	// It panics only if the OS random source is unavailable, which is unrecoverable.
	_, _ = rand.Read(b[:])
	// UUID v4: version bits
	b[6] = (b[6] & 0x0f) | 0x40
	// UUID v4: variant bits
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s_%x", timestamp, b)
}
