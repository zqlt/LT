















package bind

import "github.com/ethereum/go-ethereum/accounts/abi"


type tmplData struct {
	Package   string                   
	Contracts map[string]*tmplContract 
	Libraries map[string]string        
	Structs   map[string]*tmplStruct   
}


type tmplContract struct {
	Type        string                 
	InputABI    string                 
	InputBin    string                 
	FuncSigs    map[string]string      
	Constructor abi.Method             
	Calls       map[string]*tmplMethod 
	Transacts   map[string]*tmplMethod 
	Fallback    *tmplMethod            
	Receive     *tmplMethod            
	Events      map[string]*tmplEvent  
	Libraries   map[string]string      
	Library     bool                   
}



type tmplMethod struct {
	Original   abi.Method 
	Normalized abi.Method 
	Structured bool       
}



type tmplEvent struct {
	Original   abi.Event 
	Normalized abi.Event 
}



type tmplField struct {
	Type    string   
	Name    string   
	SolKind abi.Type 
}



type tmplStruct struct {
	Name   string       
	Fields []*tmplField 
}



var tmplSource = map[Lang]string{
	LangGo:   tmplSourceGo,
	LangJava: tmplSourceJava,
}



const tmplSourceGo = `



package {{.Package}}

import (
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)


var (
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

{{$structs := .Structs}}
{{range $structs}}
	
	type {{.Name}} struct {
	{{range $field := .Fields}}
	{{$field.Name}} {{$field.Type}}{{end}}
	}
{{end}}

{{range $contract := .Contracts}}
	
	const {{.Type}}ABI = "{{.InputABI}}"

	{{if $contract.FuncSigs}}
		
		var {{.Type}}FuncSigs = map[string]string{
			{{range $strsig, $binsig := .FuncSigs}}"{{$binsig}}": "{{$strsig}}",
			{{end}}
		}
	{{end}}

	{{if .InputBin}}
		
		var {{.Type}}Bin = "0x{{.InputBin}}"

		
		func Deploy{{.Type}}(auth *bind.TransactOpts, backend bind.ContractBackend {{range .Constructor.Inputs}}, {{.Name}} {{bindtype .Type $structs}}{{end}}) (common.Address, *types.Transaction, *{{.Type}}, error) {
		  parsed, err := abi.JSON(strings.NewReader({{.Type}}ABI))
		  if err != nil {
		    return common.Address{}, nil, nil, err
		  }
		  {{range $pattern, $name := .Libraries}}
			{{decapitalise $name}}Addr, _, _, _ := Deploy{{capitalise $name}}(auth, backend)
			{{$contract.Type}}Bin = strings.Replace({{$contract.Type}}Bin, "__${{$pattern}}$__", {{decapitalise $name}}Addr.String()[2:], -1)
		  {{end}}
		  address, tx, contract, err := bind.DeployContract(auth, parsed, common.FromHex({{.Type}}Bin), backend {{range .Constructor.Inputs}}, {{.Name}}{{end}})
		  if err != nil {
		    return common.Address{}, nil, nil, err
		  }
		  return address, tx, &{{.Type}}{ {{.Type}}Caller: {{.Type}}Caller{contract: contract}, {{.Type}}Transactor: {{.Type}}Transactor{contract: contract}, {{.Type}}Filterer: {{.Type}}Filterer{contract: contract} }, nil
		}
	{{end}}

	
	type {{.Type}} struct {
	  {{.Type}}Caller     
	  {{.Type}}Transactor 
	  {{.Type}}Filterer   
	}

	
	type {{.Type}}Caller struct {
	  contract *bind.BoundContract 
	}

	
	type {{.Type}}Transactor struct {
	  contract *bind.BoundContract 
	}

	
	type {{.Type}}Filterer struct {
	  contract *bind.BoundContract 
	}

	
	
	type {{.Type}}Session struct {
	  Contract     *{{.Type}}        
	  CallOpts     bind.CallOpts     
	  TransactOpts bind.TransactOpts 
	}

	
	
	type {{.Type}}CallerSession struct {
	  Contract *{{.Type}}Caller 
	  CallOpts bind.CallOpts    
	}

	
	
	type {{.Type}}TransactorSession struct {
	  Contract     *{{.Type}}Transactor 
	  TransactOpts bind.TransactOpts    
	}

	
	type {{.Type}}Raw struct {
	  Contract *{{.Type}} 
	}

	
	type {{.Type}}CallerRaw struct {
		Contract *{{.Type}}Caller 
	}

	
	type {{.Type}}TransactorRaw struct {
		Contract *{{.Type}}Transactor 
	}

	
	func New{{.Type}}(address common.Address, backend bind.ContractBackend) (*{{.Type}}, error) {
	  contract, err := bind{{.Type}}(address, backend, backend, backend)
	  if err != nil {
	    return nil, err
	  }
	  return &{{.Type}}{ {{.Type}}Caller: {{.Type}}Caller{contract: contract}, {{.Type}}Transactor: {{.Type}}Transactor{contract: contract}, {{.Type}}Filterer: {{.Type}}Filterer{contract: contract} }, nil
	}

	
	func New{{.Type}}Caller(address common.Address, caller bind.ContractCaller) (*{{.Type}}Caller, error) {
	  contract, err := bind{{.Type}}(address, caller, nil, nil)
	  if err != nil {
	    return nil, err
	  }
	  return &{{.Type}}Caller{contract: contract}, nil
	}

	
	func New{{.Type}}Transactor(address common.Address, transactor bind.ContractTransactor) (*{{.Type}}Transactor, error) {
	  contract, err := bind{{.Type}}(address, nil, transactor, nil)
	  if err != nil {
	    return nil, err
	  }
	  return &{{.Type}}Transactor{contract: contract}, nil
	}

	
 	func New{{.Type}}Filterer(address common.Address, filterer bind.ContractFilterer) (*{{.Type}}Filterer, error) {
 	  contract, err := bind{{.Type}}(address, nil, nil, filterer)
 	  if err != nil {
 	    return nil, err
 	  }
 	  return &{{.Type}}Filterer{contract: contract}, nil
 	}

	
	func bind{{.Type}}(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	  parsed, err := abi.JSON(strings.NewReader({{.Type}}ABI))
	  if err != nil {
	    return nil, err
	  }
	  return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
	}

	
	
	
	
	func (_{{$contract.Type}} *{{$contract.Type}}Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
		return _{{$contract.Type}}.Contract.{{$contract.Type}}Caller.contract.Call(opts, result, method, params...)
	}

	
	
	func (_{{$contract.Type}} *{{$contract.Type}}Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
		return _{{$contract.Type}}.Contract.{{$contract.Type}}Transactor.contract.Transfer(opts)
	}

	
	func (_{{$contract.Type}} *{{$contract.Type}}Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
		return _{{$contract.Type}}.Contract.{{$contract.Type}}Transactor.contract.Transact(opts, method, params...)
	}

	
	
	
	
	func (_{{$contract.Type}} *{{$contract.Type}}CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
		return _{{$contract.Type}}.Contract.contract.Call(opts, result, method, params...)
	}

	
	
	func (_{{$contract.Type}} *{{$contract.Type}}TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
		return _{{$contract.Type}}.Contract.contract.Transfer(opts)
	}

	
	func (_{{$contract.Type}} *{{$contract.Type}}TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
		return _{{$contract.Type}}.Contract.contract.Transact(opts, method, params...)
	}

	{{range .Calls}}
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Caller) {{.Normalized.Name}}(opts *bind.CallOpts {{range .Normalized.Inputs}}, {{.Name}} {{bindtype .Type $structs}} {{end}}) ({{if .Structured}}struct{ {{range .Normalized.Outputs}}{{.Name}} {{bindtype .Type $structs}};{{end}} },{{else}}{{range .Normalized.Outputs}}{{bindtype .Type $structs}},{{end}}{{end}} error) {
			var out []interface{}
			err := _{{$contract.Type}}.contract.Call(opts, &out, "{{.Original.Name}}" {{range .Normalized.Inputs}}, {{.Name}}{{end}})
			{{if .Structured}}
			outstruct := new(struct{ {{range .Normalized.Outputs}} {{.Name}} {{bindtype .Type $structs}}; {{end}} })
			{{range $i, $t := .Normalized.Outputs}} 
			outstruct.{{.Name}} = out[{{$i}}].({{bindtype .Type $structs}}){{end}}

			return *outstruct, err
			{{else}}
			if err != nil {
				return {{range $i, $_ := .Normalized.Outputs}}*new({{bindtype .Type $structs}}), {{end}} err
			}
			{{range $i, $t := .Normalized.Outputs}}
			out{{$i}} := *abi.ConvertType(out[{{$i}}], new({{bindtype .Type $structs}})).(*{{bindtype .Type $structs}}){{end}}
			
			return {{range $i, $t := .Normalized.Outputs}}out{{$i}}, {{end}} err
			{{end}}
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Session) {{.Normalized.Name}}({{range $i, $_ := .Normalized.Inputs}}{{if ne $i 0}},{{end}} {{.Name}} {{bindtype .Type $structs}} {{end}}) ({{if .Structured}}struct{ {{range .Normalized.Outputs}}{{.Name}} {{bindtype .Type $structs}};{{end}} }, {{else}} {{range .Normalized.Outputs}}{{bindtype .Type $structs}},{{end}} {{end}} error) {
		  return _{{$contract.Type}}.Contract.{{.Normalized.Name}}(&_{{$contract.Type}}.CallOpts {{range .Normalized.Inputs}}, {{.Name}}{{end}})
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}CallerSession) {{.Normalized.Name}}({{range $i, $_ := .Normalized.Inputs}}{{if ne $i 0}},{{end}} {{.Name}} {{bindtype .Type $structs}} {{end}}) ({{if .Structured}}struct{ {{range .Normalized.Outputs}}{{.Name}} {{bindtype .Type $structs}};{{end}} }, {{else}} {{range .Normalized.Outputs}}{{bindtype .Type $structs}},{{end}} {{end}} error) {
		  return _{{$contract.Type}}.Contract.{{.Normalized.Name}}(&_{{$contract.Type}}.CallOpts {{range .Normalized.Inputs}}, {{.Name}}{{end}})
		}
	{{end}}

	{{range .Transacts}}
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Transactor) {{.Normalized.Name}}(opts *bind.TransactOpts {{range .Normalized.Inputs}}, {{.Name}} {{bindtype .Type $structs}} {{end}}) (*types.Transaction, error) {
			return _{{$contract.Type}}.contract.Transact(opts, "{{.Original.Name}}" {{range .Normalized.Inputs}}, {{.Name}}{{end}})
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Session) {{.Normalized.Name}}({{range $i, $_ := .Normalized.Inputs}}{{if ne $i 0}},{{end}} {{.Name}} {{bindtype .Type $structs}} {{end}}) (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.{{.Normalized.Name}}(&_{{$contract.Type}}.TransactOpts {{range $i, $_ := .Normalized.Inputs}}, {{.Name}}{{end}})
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}TransactorSession) {{.Normalized.Name}}({{range $i, $_ := .Normalized.Inputs}}{{if ne $i 0}},{{end}} {{.Name}} {{bindtype .Type $structs}} {{end}}) (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.{{.Normalized.Name}}(&_{{$contract.Type}}.TransactOpts {{range $i, $_ := .Normalized.Inputs}}, {{.Name}}{{end}})
		}
	{{end}}

	{{if .Fallback}} 
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Transactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
			return _{{$contract.Type}}.contract.RawTransact(opts, calldata)
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Session) Fallback(calldata []byte) (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.Fallback(&_{{$contract.Type}}.TransactOpts, calldata)
		}
	
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}TransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.Fallback(&_{{$contract.Type}}.TransactOpts, calldata)
		}
	{{end}}

	{{if .Receive}} 
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Transactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
			return _{{$contract.Type}}.contract.RawTransact(opts, nil) 
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Session) Receive() (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.Receive(&_{{$contract.Type}}.TransactOpts)
		}
	
		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}TransactorSession) Receive() (*types.Transaction, error) {
		  return _{{$contract.Type}}.Contract.Receive(&_{{$contract.Type}}.TransactOpts)
		}
	{{end}}

	{{range .Events}}
		
		type {{$contract.Type}}{{.Normalized.Name}}Iterator struct {
			Event *{{$contract.Type}}{{.Normalized.Name}} 

			contract *bind.BoundContract 
			event    string              

			logs chan types.Log        
			sub  ethereum.Subscription 
			done bool                  
			fail error                 
		}
		
		
		
		func (it *{{$contract.Type}}{{.Normalized.Name}}Iterator) Next() bool {
			
			if (it.fail != nil) {
				return false
			}
			
			if (it.done) {
				select {
				case log := <-it.logs:
					it.Event = new({{$contract.Type}}{{.Normalized.Name}})
					if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
						it.fail = err
						return false
					}
					it.Event.Raw = log
					return true

				default:
					return false
				}
			}
			
			select {
			case log := <-it.logs:
				it.Event = new({{$contract.Type}}{{.Normalized.Name}})
				if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
					it.fail = err
					return false
				}
				it.Event.Raw = log
				return true

			case err := <-it.sub.Err():
				it.done = true
				it.fail = err
				return it.Next()
			}
		}
		
		func (it *{{$contract.Type}}{{.Normalized.Name}}Iterator) Error() error {
			return it.fail
		}
		
		
		func (it *{{$contract.Type}}{{.Normalized.Name}}Iterator) Close() error {
			it.sub.Unsubscribe()
			return nil
		}

		
		type {{$contract.Type}}{{.Normalized.Name}} struct { {{range .Normalized.Inputs}}
			{{capitalise .Name}} {{if .Indexed}}{{bindtopictype .Type $structs}}{{else}}{{bindtype .Type $structs}}{{end}}; {{end}}
			Raw types.Log 
		}

		
		
		
 		func (_{{$contract.Type}} *{{$contract.Type}}Filterer) Filter{{.Normalized.Name}}(opts *bind.FilterOpts{{range .Normalized.Inputs}}{{if .Indexed}}, {{.Name}} []{{bindtype .Type $structs}}{{end}}{{end}}) (*{{$contract.Type}}{{.Normalized.Name}}Iterator, error) {
			{{range .Normalized.Inputs}}
			{{if .Indexed}}var {{.Name}}Rule []interface{}
			for _, {{.Name}}Item := range {{.Name}} {
				{{.Name}}Rule = append({{.Name}}Rule, {{.Name}}Item)
			}{{end}}{{end}}

			logs, sub, err := _{{$contract.Type}}.contract.FilterLogs(opts, "{{.Original.Name}}"{{range .Normalized.Inputs}}{{if .Indexed}}, {{.Name}}Rule{{end}}{{end}})
			if err != nil {
				return nil, err
			}
			return &{{$contract.Type}}{{.Normalized.Name}}Iterator{contract: _{{$contract.Type}}.contract, event: "{{.Original.Name}}", logs: logs, sub: sub}, nil
 		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Filterer) Watch{{.Normalized.Name}}(opts *bind.WatchOpts, sink chan<- *{{$contract.Type}}{{.Normalized.Name}}{{range .Normalized.Inputs}}{{if .Indexed}}, {{.Name}} []{{bindtype .Type $structs}}{{end}}{{end}}) (event.Subscription, error) {
			{{range .Normalized.Inputs}}
			{{if .Indexed}}var {{.Name}}Rule []interface{}
			for _, {{.Name}}Item := range {{.Name}} {
				{{.Name}}Rule = append({{.Name}}Rule, {{.Name}}Item)
			}{{end}}{{end}}

			logs, sub, err := _{{$contract.Type}}.contract.WatchLogs(opts, "{{.Original.Name}}"{{range .Normalized.Inputs}}{{if .Indexed}}, {{.Name}}Rule{{end}}{{end}})
			if err != nil {
				return nil, err
			}
			return event.NewSubscription(func(quit <-chan struct{}) error {
				defer sub.Unsubscribe()
				for {
					select {
					case log := <-logs:
						
						event := new({{$contract.Type}}{{.Normalized.Name}})
						if err := _{{$contract.Type}}.contract.UnpackLog(event, "{{.Original.Name}}", log); err != nil {
							return err
						}
						event.Raw = log

						select {
						case sink <- event:
						case err := <-sub.Err():
							return err
						case <-quit:
							return nil
						}
					case err := <-sub.Err():
						return err
					case <-quit:
						return nil
					}
				}
			}), nil
		}

		
		
		
		func (_{{$contract.Type}} *{{$contract.Type}}Filterer) Parse{{.Normalized.Name}}(log types.Log) (*{{$contract.Type}}{{.Normalized.Name}}, error) {
			event := new({{$contract.Type}}{{.Normalized.Name}})
			if err := _{{$contract.Type}}.contract.UnpackLog(event, "{{.Original.Name}}", log); err != nil {
				return nil, err
			}
			return event, nil
		}

 	{{end}}
{{end}}
`



const tmplSourceJava = `



package {{.Package}};

import org.ethereum.geth.*;
import java.util.*;

{{$structs := .Structs}}
{{range $contract := .Contracts}}
{{if not .Library}}public {{end}}class {{.Type}} {
	
	public final static String ABI = "{{.InputABI}}";
	{{if $contract.FuncSigs}}
		
		public final static Map<String, String> {{.Type}}FuncSigs;
		static {
			Hashtable<String, String> temp = new Hashtable<String, String>();
			{{range $strsig, $binsig := .FuncSigs}}temp.put("{{$binsig}}", "{{$strsig}}");
			{{end}}
			{{.Type}}FuncSigs = Collections.unmodifiableMap(temp);
		}
	{{end}}
	{{if .InputBin}}
	
	public final static String BYTECODE = "0x{{.InputBin}}";

	
	public static {{.Type}} deploy(TransactOpts auth, EthereumClient client{{range .Constructor.Inputs}}, {{bindtype .Type $structs}} {{.Name}}{{end}}) throws Exception {
		Interfaces args = Geth.newInterfaces({{(len .Constructor.Inputs)}});
		String bytecode = BYTECODE;
		{{if .Libraries}}

		
		{{range $pattern, $name := .Libraries}}
		{{capitalise $name}} {{decapitalise $name}}Inst = {{capitalise $name}}.deploy(auth, client);
		bytecode = bytecode.replace("__${{$pattern}}$__", {{decapitalise $name}}Inst.Address.getHex().substring(2));
		{{end}}
		{{end}}
		{{range $index, $element := .Constructor.Inputs}}Interface arg{{$index}} = Geth.newInterface();arg{{$index}}.set{{namedtype (bindtype .Type $structs) .Type}}({{.Name}});args.set({{$index}},arg{{$index}});
		{{end}}
		return new {{.Type}}(Geth.deployContract(auth, ABI, Geth.decodeFromHex(bytecode), client, args));
	}

	
	private {{.Type}}(BoundContract deployment) {
		this.Address  = deployment.getAddress();
		this.Deployer = deployment.getDeployer();
		this.Contract = deployment;
	}
	{{end}}

	
	public final Address Address;

	
	public final Transaction Deployer;

	
	private final BoundContract Contract;

	
	public {{.Type}}(Address address, EthereumClient client) throws Exception {
		this(Geth.bindContract(address, ABI, client));
	}

	{{range .Calls}}
	{{if gt (len .Normalized.Outputs) 1}}
	
	public class {{capitalise .Normalized.Name}}Results {
		{{range $index, $item := .Normalized.Outputs}}public {{bindtype .Type $structs}} {{if ne .Name ""}}{{.Name}}{{else}}Return{{$index}}{{end}};
		{{end}}
	}
	{{end}}

	
	
	
	public {{if gt (len .Normalized.Outputs) 1}}{{capitalise .Normalized.Name}}Results{{else if eq (len .Normalized.Outputs) 0}}void{{else}}{{range .Normalized.Outputs}}{{bindtype .Type $structs}}{{end}}{{end}} {{.Normalized.Name}}(CallOpts opts{{range .Normalized.Inputs}}, {{bindtype .Type $structs}} {{.Name}}{{end}}) throws Exception {
		Interfaces args = Geth.newInterfaces({{(len .Normalized.Inputs)}});
		{{range $index, $item := .Normalized.Inputs}}Interface arg{{$index}} = Geth.newInterface();arg{{$index}}.set{{namedtype (bindtype .Type $structs) .Type}}({{.Name}});args.set({{$index}},arg{{$index}});
		{{end}}

		Interfaces results = Geth.newInterfaces({{(len .Normalized.Outputs)}});
		{{range $index, $item := .Normalized.Outputs}}Interface result{{$index}} = Geth.newInterface(); result{{$index}}.setDefault{{namedtype (bindtype .Type $structs) .Type}}(); results.set({{$index}}, result{{$index}});
		{{end}}

		if (opts == null) {
			opts = Geth.newCallOpts();
		}
		this.Contract.call(opts, results, "{{.Original.Name}}", args);
		{{if gt (len .Normalized.Outputs) 1}}
			{{capitalise .Normalized.Name}}Results result = new {{capitalise .Normalized.Name}}Results();
			{{range $index, $item := .Normalized.Outputs}}result.{{if ne .Name ""}}{{.Name}}{{else}}Return{{$index}}{{end}} = results.get({{$index}}).get{{namedtype (bindtype .Type $structs) .Type}}();
			{{end}}
			return result;
		{{else}}{{range .Normalized.Outputs}}return results.get(0).get{{namedtype (bindtype .Type $structs) .Type}}();{{end}}
		{{end}}
	}
	{{end}}

	{{range .Transacts}}
	
	
	
	public Transaction {{.Normalized.Name}}(TransactOpts opts{{range .Normalized.Inputs}}, {{bindtype .Type $structs}} {{.Name}}{{end}}) throws Exception {
		Interfaces args = Geth.newInterfaces({{(len .Normalized.Inputs)}});
		{{range $index, $item := .Normalized.Inputs}}Interface arg{{$index}} = Geth.newInterface();arg{{$index}}.set{{namedtype (bindtype .Type $structs) .Type}}({{.Name}});args.set({{$index}},arg{{$index}});
		{{end}}
		return this.Contract.transact(opts, "{{.Original.Name}}"	, args);
	}
	{{end}}

    {{if .Fallback}}
	
	
	
	public Transaction Fallback(TransactOpts opts, byte[] calldata) throws Exception { 
		return this.Contract.rawTransact(opts, calldata);
	}
    {{end}}

    {{if .Receive}}
	
	
	
	public Transaction Receive(TransactOpts opts) throws Exception { 
		return this.Contract.rawTransact(opts, null);
	}
    {{end}}
}
{{end}}
`
