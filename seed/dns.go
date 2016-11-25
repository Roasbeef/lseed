// Copyright 2016 Christian Decker. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package seed

// Various utilities to help building and serializing DNS answers. Big
// shoutout to miekg for his dns library :-)

import (
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/miekg/dns"
)

type DnsServer struct {
	netview *NetworkView
}

func NewDnsServer(netview *NetworkView) *DnsServer {
	return &DnsServer{
		netview: netview,
	}
}

func addAResponse(n Node, name string, responses *[]dns.RR) {
	header := dns.RR_Header{
		Rrtype: dns.TypeA,
		Class:  dns.ClassINET,
		Ttl:    60,
		Name:   name,
	}
	rr := &dns.A{
		Hdr: header,
		A:   n.Ip,
	}
	*responses = append(*responses, rr)
}

func addAAAAResponse(n Node, name string, responses *[]dns.RR) {
	header := dns.RR_Header{
		Rrtype: dns.TypeAAAA,
		Class:  dns.ClassINET,
		Ttl:    60,
		Name:   name,
	}
	rr := &dns.AAAA{
		Hdr:  header,
		AAAA: n.Ip,
	}
	*responses = append(*responses, rr)
}

func (ds *DnsServer) handleAAAAQuery(request *dns.Msg, response *dns.Msg) {
	nodes := ds.netview.RandomSample(3, 25)
	for _, n := range nodes {
		addAAAAResponse(n, request.Question[0].Name, &response.Answer)
	}
}

func (ds *DnsServer) handleAQuery(request *dns.Msg, response *dns.Msg) {
	nodes := ds.netview.RandomSample(2, 25)

	for _, n := range nodes {
		addAResponse(n, request.Question[0].Name, &response.Answer)
	}
}

// Handle incoming SRV requests.
//
// Unlike the A and AAAA requests these are a bit ambiguous, since the
// client may either be IPv4 or IPv6, so just return a mix and let the
// client figure it out.
func (ds *DnsServer) handleSRVQuery(request *dns.Msg, response *dns.Msg) {
	nodes := ds.netview.RandomSample(255, 25)

	header := dns.RR_Header{
		Name:   "_lightning._tcp.bitcoinstats.com.",
		Rrtype: dns.TypeSRV,
		Class:  dns.ClassINET,
		Ttl:    60,
	}

	for _, n := range nodes {
		nodeName := fmt.Sprintf("%s.%s.lseed.bitcoinstats.com.", n.Id[1:64], n.Id[64:])
		rr := &dns.SRV{
			Hdr:      header,
			Priority: 10,
			Weight:   10,
			Target:   nodeName,
			Port:     n.Port,
		}
		response.Answer = append(response.Answer, rr)

		if n.Type&1 == 1 {
			addAAAAResponse(n, nodeName, &response.Extra)
		} else {
			addAResponse(n, nodeName, &response.Extra)
		}
	}

}

func (ds *DnsServer) handleLightningDns(w dns.ResponseWriter, r *dns.Msg) {

	name := r.Question[0].Name
	qtype := r.Question[0].Qtype

	log.WithFields(log.Fields{
		"subdomain": name,
		"type":      dns.TypeToString[qtype],
	}).Debugf("Incoming request")

	m := new(dns.Msg)
	m.SetReply(r)

	if name == "lseed.bitcoinstats.com." {
		switch qtype {
		case dns.TypeAAAA:
			ds.handleAAAAQuery(r, m)
			break
		case dns.TypeA:
			ds.handleAQuery(r, m)
			break
		case dns.TypeSRV:
			ds.handleSRVQuery(r, m)
		}
	} else {
		splits := strings.SplitN(name, ".", 3)
		if len(splits) != 3 || len(splits[0])+len(splits[1]) != 65 {
			log.Debug("Subdomain does not appear to be a valid node Id")
			return
		}
		id := fmt.Sprintf("0%s%s", splits[0], splits[1])
		n, ok := ds.netview.nodes[id]
		if !ok {
			log.Debugf("Unable to find node with ID %s", id)
		} else {
			log.Debugf("Found node matching ID %s %#v", id, n)
		}

		// Reply with the correct type
		if qtype == dns.TypeAAAA {
			if n.Type&1 == 1 {
				addAAAAResponse(n, name, &m.Answer)
			} else {
				addAAAAResponse(n, name, &m.Extra)
			}
		} else if qtype == dns.TypeA {
			if n.Type&1 == 0 {
				addAResponse(n, name, &m.Answer)
			} else {
				addAResponse(n, name, &m.Extra)
			}
		}
	}
	w.WriteMsg(m)
}

func (ds *DnsServer) Serve() {
	dns.HandleFunc("lseed.bitcoinstats.com.", ds.handleLightningDns)
	server := &dns.Server{Addr: ":8053", Net: "udp"}
	if err := server.ListenAndServe(); err != nil {
		log.Errorf("Failed to setup the udp server: %s\n", err.Error())
	}
}
