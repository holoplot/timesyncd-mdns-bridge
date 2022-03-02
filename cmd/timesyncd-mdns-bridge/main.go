package main

import (
	"flag"
	"os"
	"reflect"
	"sort"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/holoplot/timesyncd-mdns-bridge/mdns"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	timesyncdDBusDestination = "org.freedesktop.timesync1"
	timesyncdDBusPath        = dbus.ObjectPath("/org/freedesktop/timesync1")
	timesyncdDBusProperty    = "org.freedesktop.timesync1.Manager.RuntimeNTPServers"
)

func main() {
	consoleWriter := zerolog.ConsoleWriter{
		Out: os.Stdout,
	}

	if isatty.IsTerminal(os.Stdout.Fd()) {
		consoleWriter.TimeFormat = time.RFC3339
	}

	log.Logger = log.Output(consoleWriter)

	serviceNameFlag := flag.String("service-name", "_ntp._udp", "mDNS service name")
	flag.Parse()

	dbusConn, err := dbus.SystemBus()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Cannot access system bus")
	}

	dbusObject := dbusConn.Object(timesyncdDBusDestination, timesyncdDBusPath)
	r, err := dbusObject.GetProperty(timesyncdDBusProperty)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Cannot get D-Bus property")
	}

	currentAddresses := r.Value().([]string)
	sort.Strings(currentAddresses)

	log.Info().
		Strs("addresses", currentAddresses).
		Msg("Current addresses in timesyncd")

	serviceTracker, err := mdns.ServiceTrackerNew(*serviceNameFlag)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Cannot create service tracker")
	}

	updateTimesyncd := func() {
		addresses := make([]string, 0)

		for _, sa := range serviceTracker.ServiceAddresses() {
			if sa.IP != nil && !sa.IP.IsLinkLocalUnicast() {
				addresses = append(addresses, sa.IP.String())
			}
		}

		sort.Strings(addresses)

		if !reflect.DeepEqual(currentAddresses, addresses) {
			log.Info().
				Strs("addresses", addresses).
				Msg("Setting new runtime NTP servers")

			err := dbusObject.Call("org.freedesktop.timesync1.Manager.SetRuntimeNTPServers", 0, addresses).Err
			if err == nil {
				currentAddresses = addresses
			} else {
				log.Warn().
					Err(err).
					Msg("Cannot set runtime NTP servers")
			}
		}
	}

	log.Info().
		Str("service_name", *serviceNameFlag).
		Msg("Monitoring services")

	for {
		select {
		case <-serviceTracker.AddCh:
			updateTimesyncd()
		case <-serviceTracker.RemoveCh:
			updateTimesyncd()
		}
	}
}
