package mdns

import (
	"fmt"
	"net"
	"sync"

	dbus "github.com/godbus/dbus/v5"
	"github.com/holoplot/go-avahi"
)

// ServiceAddress wraps an IP and a port of a resolved service
type ServiceAddress struct {
	IP             net.IP
	Port           uint16
	InterfaceIndex int32
	ServiceName    string
}

func (sa ServiceAddress) equal(other ServiceAddress) bool {
	return sa.ServiceName == other.ServiceName &&
		sa.IP.Equal(other.IP) &&
		sa.Port == other.Port &&
		sa.InterfaceIndex == other.InterfaceIndex
}

type service struct {
	avahiService avahi.Service
	cancelCh     chan struct{}
	address      ServiceAddress
}

func (service *service) dispatch(tracker *ServiceTracker) error {
	var resolver *avahi.ServiceResolver

	makeResolver := func() error {
		var err error

		if resolver != nil {
			tracker.avahiServer.ServiceResolverFree(resolver)
		}

		resolver, err = tracker.avahiServer.ServiceResolverNew(
			service.avahiService.Interface, service.avahiService.Protocol, service.avahiService.Name,
			service.avahiService.Type, service.avahiService.Domain, service.avahiService.Protocol, 0)

		return err
	}

	if err := makeResolver(); err != nil {
		return err
	}

	for {
		select {
		case resolvedService := <-resolver.FoundChannel:
			address := ServiceAddress{
				IP:             net.ParseIP(resolvedService.Address),
				Port:           resolvedService.Port,
				InterfaceIndex: resolvedService.Interface,
				ServiceName:    service.avahiService.Name,
			}

			if !address.equal(service.address) {
				if service.address.IP != nil {
					tracker.RemoveCh <- service.address
				}

				service.address = address
				tracker.AddCh <- address
			}

		case <-service.cancelCh:
			if service.address.IP != nil {
				tracker.RemoveCh <- service.address
			}

			tracker.avahiServer.ServiceResolverFree(resolver)
			return nil
		}
	}
}

func (service *service) cancel() {
	close(service.cancelCh)
}

// A ServiceTracker tracks mDNS services and provides channels to inform its users
// about updates to the internally maintained registry.
type ServiceTracker struct {
	avahiServer    *avahi.Server
	serviceBrowser *avahi.ServiceBrowser
	services       map[string]*service
	mutex          sync.Mutex
	AddCh          chan ServiceAddress
	RemoveCh       chan ServiceAddress
}

func keyForService(service avahi.Service) string {
	return fmt.Sprintf("%s.%s@%d_%d", service.Name, service.Domain, service.Interface, service.Protocol)
}

func (p *ServiceTracker) close() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for _, service := range p.services {
		service.cancel()
	}

	p.avahiServer.Close()
}

// ServiceAddresses returns a slice of currently known addresses for the tracked service
func (p *ServiceTracker) ServiceAddresses() []ServiceAddress {
	addresses := make([]ServiceAddress, 0)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	for _, service := range p.services {
		addresses = append(addresses, service.address)
	}

	return addresses
}

// ServiceTrackerNew creates a new mDNS service tracker that monitors services
// of the type given by name.
func ServiceTrackerNew(serviceType string) (*ServiceTracker, error) {
	p := new(ServiceTracker)
	p.services = make(map[string]*service)
	p.AddCh = make(chan ServiceAddress)
	p.RemoveCh = make(chan ServiceAddress)

	dbusConn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

	p.avahiServer, err = avahi.ServerNew(dbusConn)
	if err != nil {
		return nil, fmt.Errorf("avahi.ServerNew() failed: %v", err)
	}

	p.serviceBrowser, err = p.avahiServer.ServiceBrowserNew(avahi.InterfaceUnspec, avahi.ProtoUnspec, serviceType, "local", 0)
	if err != nil {
		return nil, fmt.Errorf("avahi.ServiceBrowserNew() failed: %v", err)
	}

	go func() {
		for {
			select {
			case avahiService, ok := <-p.serviceBrowser.AddChannel:
				if !ok {
					p.close()
					return
				}

				key := keyForService(avahiService)

				p.mutex.Lock()
				_, found := p.services[key]
				p.mutex.Unlock()

				if found {
					break
				}

				go func() {
					service := &service{
						avahiService: avahiService,
						cancelCh:     make(chan struct{}),
					}

					p.mutex.Lock()
					p.services[key] = service
					p.mutex.Unlock()

					service.dispatch(p)
				}()

			case avahiService, ok := <-p.serviceBrowser.RemoveChannel:
				if !ok {
					p.close()
					return
				}

				key := keyForService(avahiService)

				p.mutex.Lock()
				service, ok := p.services[key]
				if ok {
					service.cancel()
					delete(p.services, key)
				}
				p.mutex.Unlock()
			}
		}
	}()

	return p, nil
}
