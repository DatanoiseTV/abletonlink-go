module eurorack-encoder-ui

go 1.24.0

require (
	github.com/DatanoiseTV/abletonlink-go v0.0.0
	github.com/gdamore/tcell/v2 v2.8.1
	github.com/rivo/tview v0.0.0-20250501113434-0c592cd31026
	github.com/sirupsen/logrus v1.9.3
	github.com/warthog618/go-gpiocdev v0.9.0
	github.com/waxdred/go-i2c-oled v1.0.1
	golang.org/x/image v0.23.0
)

require (
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/term v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace github.com/DatanoiseTV/abletonlink-go => ../..
