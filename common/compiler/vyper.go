
















package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)


type Vyper struct {
	Path, Version, FullVersion string
	Major, Minor, Patch        int
}

func (s *Vyper) makeArgs() []string {
	p := []string{
		"-f", "combined_json",
	}
	return p
}


func VyperVersion(vyper string) (*Vyper, error) {
	if vyper == "" {
		vyper = "vyper"
	}
	var out bytes.Buffer
	cmd := exec.Command(vyper, "--version")
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	matches := versionRegexp.FindStringSubmatch(out.String())
	if len(matches) != 4 {
		return nil, fmt.Errorf("can't parse vyper version %q", out.String())
	}
	s := &Vyper{Path: cmd.Path, FullVersion: out.String(), Version: matches[0]}
	if s.Major, err = strconv.Atoi(matches[1]); err != nil {
		return nil, err
	}
	if s.Minor, err = strconv.Atoi(matches[2]); err != nil {
		return nil, err
	}
	if s.Patch, err = strconv.Atoi(matches[3]); err != nil {
		return nil, err
	}
	return s, nil
}


func CompileVyper(vyper string, sourcefiles ...string) (map[string]*Contract, error) {
	if len(sourcefiles) == 0 {
		return nil, errors.New("vyper: no source files")
	}
	source, err := slurpFiles(sourcefiles)
	if err != nil {
		return nil, err
	}
	s, err := VyperVersion(vyper)
	if err != nil {
		return nil, err
	}
	args := s.makeArgs()
	cmd := exec.Command(s.Path, append(args, sourcefiles...)...)
	return s.run(cmd, source)
}

func (s *Vyper) run(cmd *exec.Cmd, source string) (map[string]*Contract, error) {
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("vyper: %v\n%s", err, stderr.Bytes())
	}

	return ParseVyperJSON(stdout.Bytes(), source, s.Version, s.Version, strings.Join(s.makeArgs(), " "))
}










func ParseVyperJSON(combinedJSON []byte, source string, languageVersion string, compilerVersion string, compilerOptions string) (map[string]*Contract, error) {
	var output map[string]interface{}
	if err := json.Unmarshal(combinedJSON, &output); err != nil {
		return nil, err
	}

	
	contracts := make(map[string]*Contract)
	for name, info := range output {
		
		if name == "version" {
			continue
		}
		c := info.(map[string]interface{})

		contracts[name] = &Contract{
			Code:        c["bytecode"].(string),
			RuntimeCode: c["bytecode_runtime"].(string),
			Info: ContractInfo{
				Source:          source,
				Language:        "Vyper",
				LanguageVersion: languageVersion,
				CompilerVersion: compilerVersion,
				CompilerOptions: compilerOptions,
				SrcMap:          c["source_map"],
				SrcMapRuntime:   "",
				AbiDefinition:   c["abi"],
				UserDoc:         "",
				DeveloperDoc:    "",
				Metadata:        "",
			},
		}
	}
	return contracts, nil
}
