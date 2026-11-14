package ui

import (
	"slices"
	"strings"

	"github.com/marrasen/crossroads/app"
	"github.com/marrasen/crossroads/engine"
	"github.com/marrasen/crossroads/show"
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim-dmx/dmx"
	"github.com/marrasen/gunim-dmx/enttec"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
)

// The DMX boxes a DMX out offers, by the show's names for them.
var devices = []struct{ id, name string }{
	{"", "Nowhere"},
	{"enttec", "ENTTEC USB Pro"},
	{"opendmx", "ENTTEC Open DMX"},
	{"artnet", "Art-Net"},
	{"sacn", "sACN"},
}

// outputFields are a DMX out's fields: the box it sends to; for a USB
// box, its serial port and a button that looks for ports again; for
// Art-Net and sACN, the address and the universe. The Routes tab's DMX
// out box and the Board tab's output row each have them.
type outputFields struct {
	module string
	out    show.Output
	device *widget.Dropdown
	port   *widget.Dropdown
	again  *widget.IconButton
	host   *hostField
	uni    *widget.NumberField
	// ports are the ports the port list offers after "Find it", by
	// name.
	ports []string
	more  *swap
	usb   gunim.Node
	net   gunim.Node
	root  gunim.Node
}

// newOutputFields makes DMX out module's fields, in a row, or in a
// column where narrow says.
func newOutputFields(module string, narrow bool) *outputFields {
	f := &outputFields{module: module, more: &swap{}}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.name
	}
	f.device = widget.NewDropdown(widget.Labels(names...))
	f.device.OnChange = func(i int, _ *gunim.UI) gunim.Intent {
		out := f.out
		out.Device = devices[i].id
		return app.SetOutput{Module: f.module, Out: out}
	}
	f.port = widget.NewDropdown(nil)
	f.port.OnChange = func(i int, _ *gunim.UI) gunim.Intent {
		out := f.out
		out.Port = ""
		if i > 0 && i <= len(f.ports) {
			out.Port = f.ports[i-1]
		}
		return app.SetOutput{Module: f.module, Out: out}
	}
	f.again = widget.NewIconButton(icon.RefreshCw, "Look for ports again")
	f.again.OnClick = widget.Sends(app.FindPorts{})
	f.host = &hostField{TextField: widget.NewTextField()}
	f.host.OnCommit = func(text string, _ *gunim.UI) gunim.Intent { return f.setHost(text) }
	f.uni = widget.NewNumberField(0, 63999)
	f.uni.NoDrag = true
	f.uni.Tooltip = "Universe"
	f.uni.OnCommit = func(v float64, _ *gunim.UI) gunim.Intent {
		out := f.out
		out.Universe = int(v)
		return app.SetOutput{Module: f.module, Out: out}
	}
	usb := widget.Row(f.port, f.again)
	usb.Gap, usb.Cross = rowGap, widget.CrossCenter
	usb.Grow(f.port, 1)
	net := widget.Row(f.host, faint("Universe"), widget.NewSized(f.uni, 72, 0))
	net.Gap, net.Cross = rowGap, widget.CrossCenter
	net.Grow(f.host, 1)
	f.usb, f.net = usb, net
	if narrow {
		f.root = stack(f.device, f.more)
	} else {
		r := line(widget.NewSized(f.device, 180, 0), widget.NewSized(f.more, 300, 0))
		f.root = r
	}
	return f
}

// setHost is what an address typed asks for: the output at it, nil
// where it is the same.
func (f *outputFields) setHost(text string) gunim.Intent {
	text = strings.TrimSpace(text)
	if text == f.out.Host {
		return nil
	}
	out := f.out
	out.Host = text
	return app.SetOutput{Module: f.module, Out: out}
}

// set shows output o, the box of DMX out module, with ports the ports
// found; a field being typed in keeps what is typed.
func (f *outputFields) set(module string, o show.Output, ports []enttec.PortInfo, u *gunim.UI) {
	f.module, f.out = module, o
	for i, d := range devices {
		if d.id == o.Device {
			f.device.SetSelected(i, u)
		}
	}
	switch o.Device {
	case "enttec", "opendmx":
		f.ports = f.ports[:0]
		labels := []string{"Find it"}
		for _, p := range ports {
			f.ports = append(f.ports, p.Name)
			labels = append(labels, p.Label())
		}
		if o.Port != "" && !slices.Contains(f.ports, o.Port) {
			// Kept, though it is not plugged in.
			f.ports = append(f.ports, o.Port)
			labels = append(labels, o.Port+" (not plugged in)")
		}
		f.port.SetItems(widget.Labels(labels...))
		f.port.SetSelected(slices.Index(f.ports, o.Port)+1, u)
		f.more.show(f.usb, u)
	case "artnet", "sacn":
		f.host.Placeholder = "IP address"
		f.uni.Min, f.uni.Max = 0, 32767
		if o.Device == "sacn" {
			f.host.Placeholder = "Multicast"
			f.uni.Min, f.uni.Max = 1, 63999
		}
		if u == nil || !u.HasFocus(f.host) {
			f.host.SetText(o.Host, u)
		}
		if u == nil || !u.HasFocus(f.uni) {
			f.uni.SetValue(float64(max(o.Universe, int(f.uni.Min))), u)
		}
		f.more.show(f.net, u)
	default:
		f.more.show(nil, u)
	}
}

// hostField is the address field: what is typed is sent as Enter is
// pressed, or as the field is left.
type hostField struct {
	*widget.TextField
}

// Handle implements [gunim.Handler]: leaving the field commits it, as
// Enter does.
func (h *hostField) Handle(e input.Event, u *gunim.UI) bool {
	if _, ok := e.(input.FocusLost); ok && h.OnCommit != nil {
		if in := h.OnCommit(h.Text(), u); in != nil {
			u.Send(h, in)
		}
	}
	return h.TextField.Handle(e, u)
}

// outSays is what DMX out m's box does now, as l has it: it sends
// nowhere, is not connected, connects, or why it failed; or, where it
// is connected, what to.
func outSays(m *show.Module, l engine.Live) (says string, connected bool) {
	st, ok := l.Outs[m.ID]
	switch {
	case m.Out.Device == "":
		return "Sends nowhere", false
	case !ok || st.Status == dmx.Disconnected:
		return "Not connected", false
	case st.Status == dmx.Failed:
		return st.Err, false
	case st.Status == dmx.Connecting:
		return "Connecting…", false
	}
	return st.Info.Label(), true
}
