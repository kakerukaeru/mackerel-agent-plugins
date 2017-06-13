package mpaccesslog

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Songmu/axslogparser"
	"github.com/Songmu/postailer"
	mp "github.com/mackerelio/go-mackerel-plugin"
	"github.com/mackerelio/golib/pluginutil"
	"github.com/montanaflynn/stats"
)

// AccesslogPlugin mackerel plugin
type AccesslogPlugin struct {
	prefix  string
	file    string
	posFile string
}

// MetricKeyPrefix interface for PluginWithPrefix
func (p *AccesslogPlugin) MetricKeyPrefix() string {
	if p.prefix == "" {
		p.prefix = "accesslog"
	}
	return p.prefix
}

// GraphDefinition interface for mackerelplugin
func (p *AccesslogPlugin) GraphDefinition() map[string]mp.Graphs {
	labelPrefix := strings.Title(p.prefix)
	return map[string]mp.Graphs{
		"access_num": {
			Label: labelPrefix + " Access Num",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "total_count", Label: "Total Count"},
				{Name: "5xx_count", Label: "HTTP 5xx Count", Stacked: true},
				{Name: "4xx_count", Label: "HTTP 4xx Count", Stacked: true},
				{Name: "3xx_count", Label: "HTTP 3xx Count", Stacked: true},
				{Name: "2xx_count", Label: "HTTP 2xx Count", Stacked: true},
			},
		},
		"access_rate": {
			Label: labelPrefix + " Access Rate",
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "5xx_percentage", Label: "HTTP 5xx Percentage", Stacked: true},
				{Name: "4xx_percentage", Label: "HTTP 4xx Percentage", Stacked: true},
				{Name: "3xx_percentage", Label: "HTTP 3xx Percentage", Stacked: true},
				{Name: "2xx_percentage", Label: "HTTP 2xx Percentage", Stacked: true},
			},
		},
		"latency": {
			Label: labelPrefix + " Latency",
			Unit:  "float",
			Metrics: []mp.Metrics{
				{Name: "99_percentile", Label: "99 Percentile"},
				{Name: "95_percentile", Label: "95 Percentile"},
				{Name: "90_percentile", Label: "90 Percentile"},
				{Name: "average", Label: "Average"},
			},
		},
	}
}

func (p *AccesslogPlugin) getPos() string {
	base := p.file + ".pos.json"
	if p.posFile != "" {
		if filepath.IsAbs(p.posFile) {
			return p.posFile
		}
		base = p.posFile
	}
	return filepath.Join(pluginutil.PluginWorkDir(), "mackerel-plugin-accesslog.d", base)
}

// FetchMetrics interface for mackerelplugin
func (p *AccesslogPlugin) FetchMetrics() (map[string]float64, error) {
	countMetrics := []string{"total_count", "2xx_count", "3xx_count", "4xx_count", "5xx_count"}
	ret := make(map[string]float64)
	for _, k := range countMetrics {
		ret[k] = 0
	}
	var reqtimes []float64
	var psr axslogparser.Parser
	posfile := p.getPos()
	fi, err := os.Stat(posfile)
	// don't output count metrics when the pos file doesn't exist or is too old
	takeCount := err == nil && fi.ModTime().After(time.Now().Add(-2*time.Minute))
	pt, err := postailer.Open(p.file, posfile)
	if err != nil {
		return nil, err
	}
	defer pt.Close()
	s := bufio.NewScanner(pt)
	for s.Scan() {
		var l *axslogparser.Log
		var err error
		line := s.Text()
		if psr == nil {
			psr, l, err = axslogparser.GuessParser(line)
		} else {
			l, err = psr.Parse(line)
		}
		if err != nil {
			log.Println(err)
			continue
		}
		st := l.Status
		if 200 <= st && st < 300 {
			ret["2xx_count"]++
		} else if 300 <= st && st < 400 {
			ret["3xx_count"]++
		} else if 400 <= st && st < 500 {
			ret["4xx_count"]++
		} else if 500 <= st && st < 600 {
			ret["5xx_count"]++
		}
		ret["total_count"]++

		if l.ReqTime != nil {
			reqtimes = append(reqtimes, *l.ReqTime)
		} else if l.TakenSec != nil {
			reqtimes = append(reqtimes, *l.TakenSec)
		}
	}
	if s.Err() != nil {
		log.Println(s.Err())
	}
	if ret["total_count"] > 0 {
		for _, v := range []string{"2xx", "3xx", "4xx", "5xx"} {
			ret[v+"_percentage"] = ret[v+"_count"] * 100 / ret["total_count"]
		}
	}
	if len(reqtimes) > 0 {
		ret["average"], _ = stats.Mean(reqtimes)
		for _, v := range []int{90, 95, 99} {
			ret[fmt.Sprintf("%d", v)+"_percentile"], _ = stats.Percentile(reqtimes, float64(v))
		}
	}
	if !takeCount {
		for _, k := range countMetrics {
			delete(ret, k)
		}
	}
	return ret, nil
}

// Do the plugin
func Do() {
	var (
		optPrefix  = flag.String("metric-key-prefix", "", "Metric key prefix")
		optPosFile = flag.String("posfile", "", "(not necessary to specify it in the usual use case) posfile")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION] /path/to/access.log\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}
	mp.NewMackerelPlugin(&AccesslogPlugin{
		prefix:  *optPrefix,
		file:    flag.Args()[0],
		posFile: *optPosFile,
	}).Run()
}
