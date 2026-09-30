package platformserver

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

type CodeCompiler interface {
	Compile(context.Context, platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error)
}
type ContainerCompiler struct{ GoImage, TinyGoImage string }

func EnvironmentCompiler() CodeCompiler {
	if socket := os.Getenv("PLATFORM_CODE_BUILDER_SOCKET"); socket != "" {
		return socketCodeCompiler{socket}
	}
	return ContainerCompiler{GoImage: os.Getenv("PLATFORM_GO_WASM_IMAGE"), TinyGoImage: os.Getenv("PLATFORM_TINYGO_WASM_IMAGE")}
}

type codeCompilerResponse struct {
	Result platform.CodeBuildResult `json:"result"`
	Module []byte                   `json:"module,omitempty"`
	Error  string                   `json:"error,omitempty"`
}
type socketCodeCompiler struct{ socket string }

func (s socketCodeCompiler) Compile(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error) {
	raw, _ := json.Marshal(q)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.socket)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://builder/compile", bytes.NewReader(raw))
	if err != nil {
		return platform.CodeBuildResult{}, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return platform.CodeBuildResult{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return platform.CodeBuildResult{}, nil, fmt.Errorf("isolated compiler is busy or refused input")
	}
	var answer codeCompilerResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 24<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return answer.Result, nil, err
	}
	if answer.Error != "" {
		return answer.Result, nil, errors.New(answer.Error)
	}
	return answer.Result, answer.Module, nil
}

// The build driver owns only container creation. It never executes Wasm or
// receives business/database credentials; compiler guests inherit no socket.
func ServeCodeBuilder(ctx context.Context, socket string, compiler ContainerCompiler) error {
	if socket == "" || !pinnedImage.MatchString(compiler.GoImage) || !pinnedImage.MatchString(compiler.TinyGoImage) {
		return fmt.Errorf("code builder requires a Unix socket and both pinned toolchains")
	}
	listener, err := listenComputeSocket(socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0660); err != nil {
		return err
	}
	slots := make(chan struct{}, 2)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /compile", func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "compiler capacity", http.StatusTooManyRequests)
			return
		}
		var q platform.CodeBuildRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
		d.DisallowUnknownFields()
		if d.Decode(&q) != nil {
			http.Error(w, "invalid build input", http.StatusBadRequest)
			return
		}
		buildCtx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		result, module, err := compiler.Compile(buildCtx, q)
		answer := codeCompilerResponse{Result: result, Module: module}
		if err != nil {
			answer.Error = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(answer)
	})
	server := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 185 * time.Second, WriteTimeout: 185 * time.Second, MaxHeaderBytes: 4096}
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(stop)
		close(stopped)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}

var pinnedImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[0-9a-f]{64}$`)

func (c ContainerCompiler) Compile(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error) {
	var result platform.CodeBuildResult
	hash := sha256.Sum256([]byte(q.Source))
	result.SourceHash = hex.EncodeToString(hash[:])
	result.BuildHash, _ = canonicalDigest(q)
	image := c.GoImage
	if q.Language == "tinygo" {
		image = c.TinyGoImage
	}
	result.Toolchain = image
	if !pinnedImage.MatchString(image) {
		return result, nil, fmt.Errorf("%s compiler requires an owner-configured toolchain image pinned by sha256", q.Language)
	}
	if q.Language != "go" && q.Language != "tinygo" || len(q.Source) == 0 || len(q.Source) > 256<<10 {
		return result, nil, fmt.Errorf("source requires a bounded Go/TinyGo profile")
	}
	sdk, err := GenerateComputeSDK(q.Input, q.Output)
	if err != nil {
		return result, nil, err
	}
	source, err := format.Source([]byte(q.Source))
	if err != nil {
		return result, nil, fmt.Errorf("source: %w", err)
	}
	// Fixed commands and an offline stdlib-only package. Build scripts, module
	// downloads, mounts, credentials and the daemon socket never enter the guest.
	command := "go build -trimpath -ldflags='-s -w' -o /tmp/module.wasm . >&2 && cat /tmp/module.wasm"
	if q.Language == "tinygo" {
		command = "tinygo build -target=wasip1 -o /tmp/module.wasm . >&2 && cat /tmp/module.wasm"
	}
	command = "mkdir /tmp/src && tar -xf - -C /tmp/src && cd /tmp/src && " + command
	args := []string{"create", "--interactive", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--memory-swap=1g", "--cpus=2", "--user=65534:65534", "--tmpfs=/tmp:rw,nosuid,nodev,size=768m,mode=1777", "--workdir=/tmp", "--env=GOPROXY=off", "--env=GOSUMDB=off", "--env=GO111MODULE=off", "--env=GOWORK=off", "--env=GOENV=off", "--env=CGO_ENABLED=0", "--env=GOOS=wasip1", "--env=GOARCH=wasm", "--env=GOCACHE=/tmp/cache", "--env=XDG_CACHE_HOME=/tmp/cache", "--env=GOPATH=/tmp/gopath", image, "sh", "-c", command}
	created, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return result, nil, fmt.Errorf("create isolated compiler: %w", err)
	}
	id := strings.TrimSpace(string(created))
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec.CommandContext(cleanup, "docker", "rm", "-f", id).Run()
	}()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, f := range []struct {
		name string
		data []byte
	}{{"function.go", source}, {"sdk.go", sdk}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0444, Size: int64(len(f.data))}); err != nil {
			return result, nil, err
		}
		tw.Write(f.data)
	}
	tw.Close()
	module := &boundedBuffer{limit: 16 << 20}
	diagnostics := &boundedBuffer{limit: 16 << 10}
	run := exec.CommandContext(ctx, "docker", "start", "--attach", "--interactive", id)
	run.Stdin = &archive
	run.Stdout = module
	run.Stderr = diagnostics
	err = run.Run()
	result.Diagnostics = diagnostics.buf.String()
	if err != nil || module.exceeded {
		return result, nil, fmt.Errorf("isolated %s compilation failed: %s", q.Language, result.Diagnostics)
	}
	if len(module.buf.Bytes()) < 8 || string(module.buf.Bytes()[:4]) != "\x00asm" {
		return result, nil, fmt.Errorf("compiler did not produce a Wasm module")
	}
	return result, module.buf.Bytes(), nil
}

// GenerateComputeSDK emits explicit typed decoding/encoding. TinyGo uses no
// reflection-based encoding/json path. The owner's schema remains authoritative.
func GenerateComputeSDK(input, output platform.ValueSchema) ([]byte, error) {
	if err := input.Check(); err != nil {
		return nil, err
	}
	if err := output.Check(); err != nil {
		return nil, err
	}
	g := sdkGenerator{}
	g.types = &strings.Builder{}
	g.code = &strings.Builder{}
	g.schema("Input", input)
	g.schema("Output", output)
	source := "package main\nimport(\"bytes\";\"errors\";\"io\";\"os\";\"strconv\";\"unicode/utf8\")\n" + g.types.String() + sdkHelpers + g.code.String() + `
func main(){raw,err:=io.ReadAll(io.LimitReader(os.Stdin,1048577));if err==nil&&len(raw)>1048576{err=errors.New("input limit")};var input Input;if err==nil{input,err=decodeInput(raw)};var output Output;if err==nil{output,err=Run(input)};if err!=nil{os.Stdout.Write(append(append([]byte("{\"ok\":false,\"error\":{\"code\":\"compute\",\"message\":"),jsonQuote(err.Error())...),[]byte("}}")...));return};encoded:=encodeOutput(output);os.Stdout.Write(append(append([]byte("{\"ok\":true,\"value\":"),encoded...),'}'))}
`
	return format.Source([]byte(source))
}

type sdkGenerator struct{ types, code *strings.Builder }

func (g sdkGenerator) schema(name string, s platform.ValueSchema) string {
	typeName := name
	if s.Nullable {
		s.Nullable = false
		child := g.schema(name+"Value", s)
		fmt.Fprintf(g.types, "type %s = *%s\n", name, child)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(%s,error){if bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){return nil,nil};v,err:=decode%s(raw);if err!=nil{return nil,err};return &v,nil}\nfunc encode%s(v %s)[]byte{if v==nil{return []byte(\"null\")};return encode%s(*v)}\n", name, name, name+"Value", name, name, name+"Value")
		return name
	}
	switch s.Type {
	case "variant":
		keys := make([]string, 0, len(s.Variants))
		for tag := range s.Variants {
			keys = append(keys, tag)
		}
		slices.Sort(keys)
		fields := map[string]string{}
		seen := map[string]bool{}
		for i, tag := range keys {
			field := "As" + exportName(tag)
			if seen[field] {
				field += strconv.Itoa(i)
			}
			seen[field] = true
			fields[tag] = field
			g.schema(name+field, s.Variants[tag])
		}
		fmt.Fprintf(g.types, "type %s struct{Variant string;", name)
		for _, tag := range keys {
			fmt.Fprintf(g.types, "%s *%s%s;", fields[tag], name, fields[tag])
		}
		g.types.WriteString("}\n")
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){fields,err:=jsonFields(raw);if err!=nil{return out,err};tag,err:=jsonString(fields[%q]);if err!=nil{return out,err};out.Variant=tag;switch tag{", name, name, s.Discriminator)
		for _, tag := range keys {
			fmt.Fprintf(g.code, "case %q:v,err:=decode%s%s(raw);if err!=nil{return out,err};out.%s=&v;return out,nil;", tag, name, fields[tag], fields[tag])
		}
		g.code.WriteString("};return out,errors.New(\"unknown variant\")}\n")
		fmt.Fprintf(g.code, "func encode%s(v %s)[]byte{switch v.Variant{", name, name)
		for _, tag := range keys {
			fmt.Fprintf(g.code, "case %q:if v.%s!=nil{copy:=*v.%s;copy.%s=%q;return encode%s%s(copy)};", tag, fields[tag], fields[tag], exportName(s.Discriminator), tag, name, fields[tag])
		}
		g.code.WriteString("};return []byte(\"!invalid variant!\")}\n")
	case "object":
		keys := make([]string, 0, len(s.Properties))
		for key := range s.Properties {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		childTypes := map[string]string{}
		for _, key := range keys {
			childTypes[key] = g.schema(name+exportName(key), s.Properties[key])
		}
		fmt.Fprintf(g.types, "type %s struct{", name)
		for _, key := range keys {
			fmt.Fprintf(g.types, "%s %s;", exportName(key), childTypes[key])
		}
		g.types.WriteString("}\n")
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){fields,err:=jsonFields(raw);if err!=nil{return out,err};", name, name)
		for _, key := range keys {
			fmt.Fprintf(g.code, "if raw,ok:=fields[%q];ok{out.%s,err=decode%s(raw);if err!=nil{return out,err}};", key, exportName(key), name+exportName(key))
		}
		g.code.WriteString("return out,nil}\n")
		fmt.Fprintf(g.code, "func encode%s(v %s)[]byte{out:=[]byte{'{'};", name, name)
		for i, key := range keys {
			if i > 0 {
				g.code.WriteString("out=append(out,',');")
			}
			fmt.Fprintf(g.code, "out=append(out,%q...);out=append(out,encode%s(v.%s)...);", strconv.Quote(key)+":", name+exportName(key), exportName(key))
		}
		g.code.WriteString("return append(out,'}')}\n")
	case "array":
		child := g.schema(name+"Item", *s.Items)
		fmt.Fprintf(g.types, "type %s []%s\n", name, child)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){items,err:=jsonItems(raw);if err!=nil{return out,err};for _,raw:=range items{v,err:=decode%s(raw);if err!=nil{return out,err};out=append(out,v)};return out,nil}\nfunc encode%s(v %s)[]byte{out:=[]byte{'['};for i,item:=range v{if i>0{out=append(out,',')};out=append(out,encode%s(item)...)};return append(out,']')}\n", name, name, name+"Item", name, name, name+"Item")
	default:
		base, parse, encode := "string", "jsonString(raw)", "jsonQuote(string(v))"
		switch s.Type {
		case "integer":
			base, parse, encode = "int64", "strconv.ParseInt(string(raw),10,64)", "strconv.AppendInt(nil,int64(v),10)"
		case "number":
			base, parse, encode = "float64", "strconv.ParseFloat(string(raw),64)", "strconv.AppendFloat(nil,float64(v),'g',-1,64)"
		case "boolean":
			base, parse, encode = "bool", "strconv.ParseBool(string(raw))", "strconv.AppendBool(nil,bool(v))"
		}
		fmt.Fprintf(g.types, "type %s = %s\n", name, base)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(%s,error){v,err:=%s;return %s(v),err}\nfunc encode%s(v %s)[]byte{return %s}\n", name, name, parse, name, name, name, encode)
	}
	return typeName
}
func exportName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

const sdkHelpers = `
func jsonQuote(s string)[]byte{out:=[]byte{'"'};const hex="0123456789abcdef";for i:=0;i<len(s);i++{c:=s[i];if c=='"'||c=='\\'{out=append(out,'\\',c)}else if c<32{out=append(out,'\\','u','0','0',hex[c>>4],hex[c&15])}else{out=append(out,c)}};return append(out,'"')}
func jsonString(raw []byte)(string,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='"'||raw[len(raw)-1]!='"'{return "",errors.New("string expected")};raw=raw[1:len(raw)-1];out:=[]byte{};for i:=0;i<len(raw);i++{c:=raw[i];if c!='\\'{out=append(out,c);continue};i++;if i>=len(raw){return "",errors.New("string escape incomplete")};switch raw[i]{case '"','\\','/':out=append(out,raw[i]);case 'b':out=append(out,8);case 'f':out=append(out,12);case 'n':out=append(out,10);case 'r':out=append(out,13);case 't':out=append(out,9);case 'u':if i+4>=len(raw){return "",errors.New("unicode escape incomplete")};n,err:=strconv.ParseUint(string(raw[i+1:i+5]),16,16);if err!=nil{return "",err};i+=4;r:=rune(n);if r>=0xd800&&r<=0xdbff{if i+6<len(raw)&&raw[i+1]=='\\'&&raw[i+2]=='u'{low,err:=strconv.ParseUint(string(raw[i+3:i+7]),16,16);if err==nil&&low>=0xdc00&&low<=0xdfff{r=0x10000+(r-0xd800)*1024+rune(low)-0xdc00;i+=6}else{r=utf8.RuneError}}else{r=utf8.RuneError}}else if r>=0xdc00&&r<=0xdfff{r=utf8.RuneError};out=utf8.AppendRune(out,r);default:return "",errors.New("invalid string escape")}};return string(out),nil}
func valueEnd(raw []byte) int {depth:=0;quoted:=false;escaped:=false;for i,c:=range raw{if quoted{if escaped{escaped=false}else if c=='\\'{escaped=true}else if c=='"'{quoted=false};continue};if c=='"'{quoted=true;continue};if c=='{'||c=='['{depth++};if c=='}'||c==']'{if depth==0{return i};depth--};if c==','&&depth==0{return i}};return len(raw)}
func jsonFields(raw []byte)(map[string][]byte,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='{'||raw[len(raw)-1]!='}'{return nil,errors.New("object expected")};raw=bytes.TrimSpace(raw[1:len(raw)-1]);out:=map[string][]byte{};for len(raw)>0{if raw[0]!='"'{return nil,errors.New("field expected")};end:=1;for end<len(raw){if raw[end]=='\\'{end+=2;continue};if raw[end]=='"'{break};end++};if end>=len(raw){return nil,errors.New("field incomplete")};key,err:=strconv.Unquote(string(raw[:end+1]));if err!=nil{return nil,err};raw=bytes.TrimSpace(raw[end+1:]);if len(raw)==0||raw[0]!=':'{return nil,errors.New("value expected")};raw=bytes.TrimSpace(raw[1:]);end=valueEnd(raw);out[key]=bytes.TrimSpace(raw[:end]);raw=bytes.TrimSpace(raw[end:]);if len(raw)>0{if raw[0]!=','{return nil,errors.New("separator expected")};raw=bytes.TrimSpace(raw[1:])}};return out,nil}
func jsonItems(raw []byte)([][]byte,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='['||raw[len(raw)-1]!=']'{return nil,errors.New("array expected")};raw=bytes.TrimSpace(raw[1:len(raw)-1]);out:=[][]byte{};for len(raw)>0{end:=valueEnd(raw);out=append(out,bytes.TrimSpace(raw[:end]));raw=bytes.TrimSpace(raw[end:]);if len(raw)>0{if raw[0]!=','{return nil,errors.New("separator expected")};raw=bytes.TrimSpace(raw[1:])}};return out,nil}
`
