package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/genshinsim/gcsim"
	"github.com/genshinsim/gcsim/internal/logtohtml"
	"github.com/genshinsim/gcsim/pkg/core"
	"github.com/genshinsim/gcsim/pkg/parse"
	"go.uber.org/zap"
)

func main() {

	var src []byte

	var err error

	pr := flag.Bool("print", true, "print output to screen? default true")
	jsonFile := flag.String("js", "", "output result to json? supply file path (otherwise empty string for disabled). default disabled")
	debug := flag.Bool("d", false, "show debug? default false")
	debugHTML := flag.Bool("dh", true, "output debug html? default true (but only matters if debug is enabled)")
	seconds := flag.Int("s", 0, "how many seconds to run the sim for")
	cfgFile := flag.String("c", "config.txt", "which profile to use")
	detailed := flag.Bool("t", true, "log combat details")
	// f := flag.String("o", "debug.log", "detailed log file")
	// hp := flag.Float64("hp", 0, "hp mode: how much hp to deal damage to")
	// showCaller := flag.Bool("caller", false, "show caller in debug low")
	// fixedRand := flag.Bool("noseed", false, "use 0 for rand seed always - guarantee same results every time; only in single mode")
	// avgMode := flag.Bool("a", false, "run sim multiple times and calculate avg damage (smooth out randomness). default false. note that there is no debug log in this mode")
	w := flag.Int("w", 0, "number of workers to run when running multiple iterations; default 24")
	i := flag.Int("i", 0, "number of iterations to run if we're running multiple")
	multi := flag.String("m", "", "mutiple config mode")
	mmMode := flag.Bool("minmax", false, "track the min/max run seed and rerun those (single mode with debug only)")
	// t := flag.Int("t", 1, "target multiplier")

	flag.Parse()
	log.Println(*debugHTML)

	if *multi != "" {
		content, err := ioutil.ReadFile(*multi)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
		files := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
		// lines := strings.Split(string(content), `\n`)
		err = runMulti(files, *w, *i)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
		return
	}

	src, err = ioutil.ReadFile(*cfgFile)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	//check for imports
	var data strings.Builder
	var re = regexp.MustCompile(`(?m)^import "(.+)"$`)

	rows := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	for _, row := range rows {
		if re.MatchString(row) {
			match := re.FindStringSubmatch(row)
			//read import
			p := path.Join(path.Dir(*cfgFile), match[1])
			src, err = ioutil.ReadFile(p)
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}

			data.WriteString(string(src))

		} else {
			data.WriteString(row)
			data.WriteString("\n")
		}
	}

	// fmt.Println(data.String())

	parser := parse.New("single", data.String())
	cfg, opts, err := parser.Parse()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	if *i > 0 {
		opts.Iteration = *i
	}
	if *w > 0 {
		opts.Workers = *w
	}
	if *debug {
		opts.Debug = true
	}
	if *seconds > 0 {
		opts.Duration = *seconds
	}
	if *detailed {
		opts.LogDetails = true
	}

	// log.Println(opts)
	// defer profile.Start(profile.ProfilePath("./")).Stop()

	var result gcsim.Result
	//if debug we're going to capture the logs
	if opts.Debug {
		r, w, err := os.Pipe()
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}

		outC := make(chan string)
		// copy the output in a separate goroutine so printing can't block indefinitely
		go func() {
			var buf bytes.Buffer
			io.Copy(&buf, r)
			outC <- buf.String()
		}()

		zap.RegisterSink("gsim", func(url *url.URL) (zap.Sink, error) {
			return w, nil
		})

		opts.DebugPaths = []string{"gsim://"}

		result, err = gcsim.Run(data.String(), opts)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}

		w.Close()

		out := <-outC

		if *debugHTML {
			chars := make([]string, len(cfg.Characters.Profile))
			for i, v := range cfg.Characters.Profile {
				chars[i] = v.Base.Name
			}
			err = logtohtml.WriteString(out, "./debug.html", cfg.Characters.Initial, chars)
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}
		}

		result.Debug = out

	} else {
		result, err = gcsim.Run(data.String(), opts)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
	}

	if *pr {
		fmt.Print(result.PrettyPrint())
	}

	if *mmMode && *debug {

		minResult, err := runSeeded(data.String(), result.MinSeed, opts, "debugmin")
		if err != nil {
			log.Panic(err)
		}
		maxResult, err := runSeeded(data.String(), result.MaxSeed, opts, "debugmax")
		if err != nil {
			log.Panic(err)
		}

		fmt.Printf("Min seed: %v | DPS: %v\n", result.MinSeed, minResult.DPS)
		fmt.Printf("Min seed: %v | DPS: %v\n", result.MaxSeed, maxResult.DPS)
	}

	if *jsonFile != "" {
		//try creating file to write to
		result.Text = result.PrettyPrint()
		data, _ := json.Marshal(result)
		err := os.WriteFile(*jsonFile, data, 0644)
		if err != nil {
			log.Panic(err)
		}
	}

}

func runSeeded(data string, seed int64, opts core.RunOpt, file string) (gcsim.Stats, error) {
	r, w, err := os.Pipe()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	outC := make(chan string)
	// copy the output in a separate goroutine so printing can't block indefinitely
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outC <- buf.String()
	}()

	zap.RegisterSink(file, func(url *url.URL) (zap.Sink, error) {
		return w, nil
	})

	opts.DebugPaths = []string{fmt.Sprintf("%v://", file)}

	parser := parse.New("single", data)
	cfg, _, _ := parser.Parse()

	sim, err := gcsim.NewSim(cfg, seed, opts)
	if err != nil {
		return gcsim.Stats{}, err
	}

	v, err := sim.Run()
	if err != nil {
		return gcsim.Stats{}, err
	}

	w.Close()

	out := <-outC

	if file != "" {

		chars := make([]string, len(cfg.Characters.Profile))
		for i, v := range cfg.Characters.Profile {
			chars[i] = v.Base.Name
		}
		err = logtohtml.WriteString(out, fmt.Sprintf("./%v.html", file), cfg.Characters.Initial, chars)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
	}

	return v, nil
}

func runMulti(files []string, w, i int) error {
	fmt.Print("Filename                                                     |      Mean|       Min|       Max|   Std Dev|   HP Mode|     Iters|\n")
	fmt.Print("--------------------------------------------------------------------------------------------------------------------------------\n")
	for _, f := range files {
		if f == "" || f[0] == '#' {
			continue
		}
		src, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}

		var data strings.Builder
		var re = regexp.MustCompile(`(?m)^import "(.+)"$`)

		rows := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
		for _, row := range rows {
			if re.MatchString(row) {
				match := re.FindStringSubmatch(row)
				//read import
				p := path.Join(path.Dir(f), match[1])
				src, err = ioutil.ReadFile(p)
				if err != nil {
					log.Fatal(err)
				}

				data.WriteString(string(src))

			} else {
				data.WriteString(row)
				data.WriteString("\n")
			}
		}

		parser := parse.New("single", data.String())
		_, opts, err := parser.Parse()
		if err != nil {
			return err
		}
		if w > 0 {
			opts.Workers = w
		}
		if i > 0 {
			opts.Iteration = i
		}
		opts.Debug = false
		opts.LogDetails = false

		fmt.Printf("%60.60v |", f)
		r, err := gcsim.Run(data.String(), opts)
		if err != nil {
			log.Fatal(err)
		}
		// log.Println(r)
		fmt.Printf("%10.2f|%10.2f|%10.2f|%10.2f|%10.10v|%10d|\n", r.DPS.Mean, r.DPS.Min, r.DPS.Max, r.DPS.SD, r.IsDamageMode, r.Iterations)
	}
	return nil
}