package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shuLhan/share/lib/dns"
)

func usage() {
	fmt.Println("Usage: " + os.Args[0] + " <nameserver> <hosts-file>")
	os.Exit(1)
}

func main() {
	if len(os.Args) < 3 {
		usage()
	}

	log.SetFlags(0)

	cl, err := dns.NewUDPClient(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	hostsFile, err := dns.ParseHostsFile(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}

	var nfail int

	fmt.Printf("= Benchmarking with %d messages\n", len(hostsFile.Records))

	timeStart := time.Now()
	for _, rr := range hostsFile.Records {
		res, err := cl.Lookup(true, rr.Type, rr.Class, rr.Name)
		if err != nil {
			nfail++
			log.Println("! Send error: ", err)
			continue
		}

		exp := rr.Value.(string)
		got := ""
		found := false
		for x := 0; x < len(res.Answer); x++ {
			got = res.Answer[x].Value.(string)
			if exp == got {
				found = true
				break
			}
		}

		if !found {
			nfail++
			log.Printf(`! Answer not matched %s:
expecting: %s
got: %s
`, rr.String(), exp, got)
		}
	}
	timeEnd := time.Now()

	fmt.Printf("= Total: %d\n", len(hostsFile.Records))
	fmt.Printf("= Failed: %d\n", nfail)
	fmt.Printf("= Elapsed time: %v\n", timeEnd.Sub(timeStart))
}
