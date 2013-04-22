// Iris - Distributed Messaging Framework
// Copyright 2013 Peter Szilagyi. All rights reserved.
//
// Iris is dual licensed: you can redistribute it and/or modify it under the
// terms of the GNU General Public License as published by the Free Software
// Foundation, either version 3 of the License, or (at your option) any later
// version.
//
// The framework is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.  See the GNU General Public License for
// more details.
//
// Alternatively, the Iris framework may be used in accordance with the terms
// and conditions contained in a signed written agreement between you and the
// author(s).
//
// Author: peterke@gmail.com (Peter Szilagyi)
package overlay

import (
	"log"
	"math/big"
	"proto/session"
)

// Routing state exchange message (leaves, neighbors and common row).
type state struct {
	Addrs map[string][]string

	Updated uint64
	Merged  uint64
}

// The extra headers the pastry requires to be functional.
type header struct {
	Dest  *big.Int
	State *state
	Meta  []byte
}

// Pastry message container for channel passing.
type message struct {
	head *header
	data *session.Message
}

// Listens on one particular session, extracts the pastry headers out of each
// inbound message and invokes the router to finish the job.
func (o *overlay) receiver(p *peer) {
	defer close(p.out)

	// Read messages until a quit is requested
	for {
		select {
		case <-o.quit:
			return
		case <-p.quit:
			return
		case pkt, ok := <-p.netIn:
			if !ok {
				return
			}
			// Extract the pastry headers
			p.inBuf.Write(pkt.Head.Meta)
			msg := &message{new(header), pkt}
			if err := p.dec.Decode(msg.head); err != nil {
				log.Printf("failed to decode pastry headers: %v.", err)
				return
			}
			o.route(p, msg)
		}
	}
}

// Sends a pastry join message to the remote peer.
func (o *overlay) sendJoin(p *peer) {
	s := new(state)

	// Ensure nodes can contact joining peer
	s.Addrs = make(map[string][]string)
	addrs := make([]string, len(o.addrs))
	copy(addrs, p.addrs)
	s.Addrs[o.nodeId.String()] = addrs

	// Send out the join request
	p.out <- &message{&header{o.nodeId, s, nil}, new(session.Message)}
}

// Sends a pastry state message to the remote peer.
func (o *overlay) sendState(p *peer) {
	s := new(state)
	s.Updated = o.time
	s.Merged = p.time

	// Searialize the leaf set, common row and neighbor list into the address map
	s.Addrs = make(map[string][]string)
	for _, id := range o.routes.leaves {
		sid := id.String()
		if id.Cmp(o.nodeId) != 0 {
			s.Addrs[sid] = append([]string{}, o.pool[sid].addrs...)
		} else {
			s.Addrs[sid] = append([]string{}, o.addrs...)
		}
	}
	idx := prefix(o.nodeId, p.self)
	for i := 0; i < len(o.routes.routes[idx]); i++ {
		if id := o.routes.routes[idx][i]; id != nil {
			sid := id.String()
			s.Addrs[sid] = append([]string{}, o.pool[sid].addrs...)
		}
	}
	for _, id := range o.routes.nears {
		sid := id.String()
		s.Addrs[sid] = append([]string{}, o.pool[sid].addrs...)
	}
	// Send everything over the wire
	head := &header{o.nodeId, s, nil}
	msg := new(session.Message)
	p.out <- &message{head, msg}
}

// Waits for outbound pastry messages, encodes them into the lower level session
// format and send them on their way.
func (o *overlay) sender(p *peer) {
	defer close(p.netOut)

	for {
		select {
		case <-o.quit:
			return
		case <-p.quit:
			return
		case msg, ok := <-p.out:
			if !ok {
				return
			}
			// Check whether header recode is needed and do if so
			if msg.head != nil {
				if err := p.enc.Encode(msg.head); err != nil {
					log.Printf("failed to encode pastry headers: %v.", err)
					return
				}
				msg.data.Head.Meta = append(msg.data.Head.Meta[:0], p.outBuf.Bytes()...)
				p.outBuf.Reset()
			}
			p.netOut <- msg.data
		}
	}
}
