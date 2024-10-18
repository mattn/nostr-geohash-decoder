package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/codingsince1985/geo-golang/openstreetmap"
	"github.com/mmcloughlin/geohash"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

func publishEvent(wg *sync.WaitGroup, r string, ev nostr.Event, success *atomic.Int64) {
	defer wg.Done()

	relay, err := nostr.RelayConnect(context.Background(), r)
	if err != nil {
		log.Println(relay.URL, err)
		return
	}
	defer relay.Close()

	err = relay.Publish(context.Background(), ev)
	if err != nil {
		log.Println(relay.URL, err)
	} else {
		success.Add(1)
	}
}

func reply(nsec string, relays []string, ev *nostr.Event, content string) error {
	tags := nostr.Tags{}
	tags = tags.AppendUnique(nostr.Tag{"t", "geohash"})

	eev := nostr.Event{}
	var sk string
	if _, s, err := nip19.Decode(nsec); err == nil {
		sk = s.(string)
	} else {
		return err
	}
	if pub, err := nostr.GetPublicKey(sk); err == nil {
		if _, err := nip19.EncodePublicKey(pub); err != nil {
			return err
		}
		eev.PubKey = pub
	} else {
		return err
	}

	eev.Kind = nostr.KindTextNote
	eev.Content = content

	eev.CreatedAt = ev.CreatedAt + 1
	eev.Kind = ev.Kind
	eev.Tags = tags
	eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"e", ev.ID, "", "reply"})
	eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"p", ev.PubKey})
	for _, te := range ev.Tags {
		if te.Key() == "e" {
			eev.Tags = eev.Tags.AppendUnique(te)
		}
	}
	err := eev.Sign(sk)
	if err != nil {
		return err
	}

	var success atomic.Int64
	var wg sync.WaitGroup
	for _, r := range relays {
		wg.Add(1)
		go publishEvent(&wg, r, eev, &success)
	}
	wg.Wait()
	if success.Load() == 0 {
		return errors.New("failed to publish")
	}
	return nil
}

func main() {
	ctx := context.Background()
	pool := nostr.NewSimplePool(ctx)
	filters := nostr.Filters{
		{
			Kinds: []int{nostr.KindTextNote},
		},
	}
	sub := pool.SubMany(ctx, []string{"wss://yabu.me"}, filters)
	for ev := range sub {
		for _, tag := range ev.Tags {
			if len(tag) != 2 || tag[0] != "g" {
				continue
			}
			lat, lng := geohash.Decode(tag[1])
			addr, err := openstreetmap.Geocoder().ReverseGeocode(lat, lng)
			if err != nil {
				continue
			}
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "FormattedAddress: %s\n", addr.FormattedAddress)
			fmt.Fprintf(&buf, "Street: %s\n", addr.Street)
			fmt.Fprintf(&buf, "HouseNumber: %s\n", addr.HouseNumber)
			fmt.Fprintf(&buf, "Suburb: %s\n", addr.Suburb)
			fmt.Fprintf(&buf, "Postcode: %s\n", addr.Postcode)
			fmt.Fprintf(&buf, "State: %s\n", addr.State)
			fmt.Fprintf(&buf, "StateCode: %s\n", addr.StateCode)
			fmt.Fprintf(&buf, "StateDistrict: %s\n", addr.StateDistrict)
			fmt.Fprintf(&buf, "County: %s\n", addr.County)
			fmt.Fprintf(&buf, "Country: %s\n", addr.Country)
			fmt.Fprintf(&buf, "CountryCode: %s\n", addr.CountryCode)
			fmt.Fprintf(&buf, "City: %s\n", addr.City)
			reply(os.Getenv("BOT_NSEC"), []string{"wss://yabu.me"}, ev.Event, buf.String())
		}
	}
}
