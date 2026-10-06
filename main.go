//go:build tinygo && rp2040

package main

import (
	"log/slog"
	"machine"
	"time"

	"github.com/soypat/cyw43439"
	"github.com/soypat/cyw43439/examples/cywnet"
	"github.com/soypat/lneto"
	"github.com/soypat/lneto/ipv4"
	"github.com/soypat/lneto/tcp"
	"github.com/soypat/lneto/x/xnet"
)

const pollInterval = 5 * time.Millisecond

func main() {
	time.Sleep(2 * time.Second) // Allow USB serial to become available.
	println("Pico W web server and DNS filter starting")
	serviceStatus.start()
	if wifiSSID == "" || wifiSSID == "YOUR_WIFI_NAME" {
		halt("set wifiSSID and wifiPassword in secrets.go, then rebuild")
	}
	logger := slog.New(slog.NewTextHandler(machine.Serial, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := cyw43439.DefaultWifiConfig()
	cfg.Logger = logger
	// The pinned cywnet helper configures the radio and the lightweight IP stack.
	// No credentials files are needed inside the downloaded dependency.
	stack, err := cywnet.NewConfiguredPicoWithStack(wifiSSID, wifiPassword, cfg, cywnet.StackConfig{
		Hostname:          "pico-webserver",
		MaxActiveTCPPorts: 1,
		MaxActiveUDPPorts: 1,
	})
	if err != nil {
		halt("Wi-Fi setup failed: " + err.Error())
	}
	// Packet polling must continue while DHCP and TCP operations wait for replies.
	go pollNetwork(stack)
	lease, err := stack.SetupWithDHCP(cywnet.DHCPConfig{})
	if err != nil {
		halt("DHCP failed: " + err.Error())
	}
	serviceStatus.setNetwork(ipv4.String(lease.AssignedAddr4))
	pool, err := xnet.NewTCPPool(xnet.TCPPoolConfig{
		PoolSize:           3,
		QueueSize:          3,
		TxBufSize:          2048,
		RxBufSize:          1024,
		EstablishedTimeout: 15 * time.Second,
		ClosingTimeout:     5 * time.Second,
		NewBackoff:         func() lneto.BackoffStrategy { return tcpBackoff },
	})
	if err != nil {
		halt("TCP pool failed: " + err.Error())
	}
	var listener tcp.Listener
	if err = listener.Reset(80, pool); err != nil {
		halt("listener failed: " + err.Error())
	}
	if err = stack.LnetoStack().RegisterListenerTCP(&listener); err != nil {
		halt("register listener failed: " + err.Error())
	}
	serviceStatus.setHTTPReady(true)
	go func() {
		if err := serveDNSServer(stack.LnetoStack()); err != nil {
			halt("DNS server failed: " + err.Error())
		}
	}()
	webAddress := "http://" + ipv4.String(lease.AssignedAddr4) + "/"
	printWebAddress(webAddress)
	println("DHCP lease seconds:", lease.TLease)
	// Restart before the lease expires: the helper performs initial DHCP only.
	// Rebooting obtains a fresh lease and also provides periodic Wi-Fi recovery.
	go func() {
		d := time.Duration(lease.TLease) * time.Second / 2
		if d <= 0 || d > 12*time.Hour {
			d = 12 * time.Hour
		}
		time.Sleep(d)
		println("Restarting to refresh Wi-Fi and DHCP")
		machine.CPUReset()
	}()
	serialWasOpen := machine.Serial.DTR()
	for {
		serialOpen := machine.Serial.DTR()
		if serialOpen && !serialWasOpen {
			printWebAddress(webAddress)
		}
		serialWasOpen = serialOpen
		pool.CheckTimeouts()
		if listener.NumberOfReadyToAccept() == 0 {
			time.Sleep(pollInterval)
			continue
		}
		conn, _, err := listener.TryAccept()
		if err != nil {
			println("accept:", err.Error())
			continue
		}
		go func() {
			if err := serveHTTP(conn); err != nil {
				println("HTTP:", err.Error())
			}
		}()
	}
}

func printWebAddress(addr string) {
	println("Open this address on another machine on your LAN:")
	println(addr)
}

func tcpBackoff(consecutive uint) time.Duration {
	return min(time.Microsecond<<min(consecutive, 13), pollInterval)
}

// Keep setup failures visible even if the monitor is opened after startup.
func halt(reason string) {
	for {
		println(reason)
		time.Sleep(5 * time.Second)
	}
}

func pollNetwork(stack *cywnet.Stack) {
	for {
		sent, received, err := stack.RecvAndSend()
		if err != nil {
			println("network:", err.Error())
		}
		if err != nil || (sent == 0 && received == 0) {
			time.Sleep(pollInterval)
		}
	}
}
