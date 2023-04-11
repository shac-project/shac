// Copyright 2023 The Shac Authors
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

package reporting

// Efficient ANSI code support.

// ansiCode is one of the ANSI escape code.
//
// It only represents the simple ones without options.
//
// https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_.28Select_Graphic_Rendition.29_parameters
//
// The colors vary a lot across terminals, this table is very useful to
// understand the contrast ratios.
// https://en.wikipedia.org/wiki/ANSI_escape_code#3-bit_and_4-bit
type ansiCode int

func (a ansiCode) String() string {
	return ansiCodeMap[a]
}

const (
	// Styling codes.
	reset ansiCode = iota
	bold
	faint
	italic
	underline
	blinkSlow
	blinkRapid
	reverseVideo
	concealed
	crossedOut

	// Foreground colors.
	fgBlack ansiCode = iota + 30
	fgRed
	fgGreen
	fgYellow
	fgBlue
	fgMagenta
	fgCyan
	fgWhite

	// Bright foreground colors.
	fgHiBlack ansiCode = iota + 90
	fgHiRed
	fgHiGreen
	fgHiYellow
	fgHiBlue
	fgHiMagenta
	fgHiCyan
	fgHiWhite

	// Background colors.
	bgBlack ansiCode = iota + 40
	bgRed
	bgGreen
	bgYellow
	bgBlue
	bgMagenta
	bgCyan
	bgWhite

	// Bright background colors.
	bgHiBlack ansiCode = iota + 100
	bgHiRed
	bgHiGreen
	bgHiYellow
	bgHiBlue
	bgHiMagenta
	bgHiCyan
	bgHiWhite
)

var ansiCodeMap = map[ansiCode]string{
	// This could be a nice for loop but we'd pay the cost at runtime both in term
	// of CPU usage and memory. Instead I personally paid the cost at editing
	// time by creating a one-off vim macro to generate these that never changed
	// since the 80s.
	//
	// Yes, this is an explicit disapproval of most ANSI Go libraries out there.
	reset:        "\x1b[0m",
	bold:         "\x1b[1m",
	faint:        "\x1b[2m",
	italic:       "\x1b[3m",
	underline:    "\x1b[4m",
	blinkSlow:    "\x1b[5m",
	blinkRapid:   "\x1b[6m",
	reverseVideo: "\x1b[7m",
	concealed:    "\x1b[8m",
	crossedOut:   "\x1b[9m",
	fgBlack:      "\x1b[30m",
	fgRed:        "\x1b[31m",
	fgGreen:      "\x1b[32m",
	fgYellow:     "\x1b[33m",
	fgBlue:       "\x1b[34m",
	fgMagenta:    "\x1b[35m",
	fgCyan:       "\x1b[36m",
	fgWhite:      "\x1b[37m",
	bgBlack:      "\x1b[40m",
	bgRed:        "\x1b[41m",
	bgGreen:      "\x1b[42m",
	bgYellow:     "\x1b[43m",
	bgBlue:       "\x1b[44m",
	bgMagenta:    "\x1b[45m",
	bgCyan:       "\x1b[46m",
	bgWhite:      "\x1b[47m",
	fgHiBlack:    "\x1b[90m",
	fgHiRed:      "\x1b[91m",
	fgHiGreen:    "\x1b[92m",
	fgHiYellow:   "\x1b[93m",
	fgHiBlue:     "\x1b[94m",
	fgHiMagenta:  "\x1b[95m",
	fgHiCyan:     "\x1b[96m",
	fgHiWhite:    "\x1b[97m",
	bgHiBlack:    "\x1b[100m",
	bgHiRed:      "\x1b[101m",
	bgHiGreen:    "\x1b[102m",
	bgHiYellow:   "\x1b[103m",
	bgHiBlue:     "\x1b[104m",
	bgHiMagenta:  "\x1b[105m",
	bgHiCyan:     "\x1b[106m",
	bgHiWhite:    "\x1b[107m",
}
