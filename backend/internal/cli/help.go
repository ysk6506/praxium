package cli

import "strings"

// The help of the client commands. Each entry is what `praxium <command> --help` prints: a usage line, what the command
// does, its flags with their defaults, and examples. The usage line is also what a wrong command line prints, with a
// pointer to the help, so a mistake never prints the whole list.

const globalFlagsHelp = `global flags (before or after the command):
  --endpoint URL   the server to ask (default http://127.0.0.1:8080; environment PRAXIUM_ENDPOINT)
  --token TOKEN    the access token (environment PRAXIUM_TOKEN). praxium demo and praxium local print theirs
  --config FILE    a JSON file {"endpoint": "...", "token": "..."} (environment PRAXIUM_CONFIG)
  --json           print JSON instead of a table
  A flag wins over its environment variable, which wins over the config file.
`

const readFlagsHelp = `read flags (the point of the KG to read, and how much):
  --kg NAME         the KG to read (default main)
  --at-seq N        read the state as of change number N
  --version ID      read an older KG version
  --view VIEW       as_observed (default) or as_designed
  --min-seq N       wait until change number N is readable (with --wait)
  --wait DURATION   how long to wait for --min-seq, such as PT5S
  --limit N         rows per page
  --all             follow every page (default: one page)
  --summary         counts only, no rows
`

type helpEntry struct {
	usage string
	body  string
}

var helpEntries = map[string]helpEntry{
	"health": {
		usage: "usage: praxium health",
		body: `Asks the server whether it is alive, ready and started. Exit code 0 when all three are fine.

example:
  praxium --endpoint http://127.0.0.1:8080 health
`},
	"types": {
		usage: "usage: praxium types list|get ...",
		body: `The types of the model: what kinds of objects and links a KG can hold.
  list   list types (--kind object|link|action|trait|scheme, --filter, --limit, --all)
  get    show one type by id or name
Run "praxium types list --help" or "praxium types get --help".
`},
	"types list": {
		usage: "usage: praxium types list [--kind K] [--filter field.op=value]... [--limit N] [--all]",
		body: `Lists the types of the model, one page by default.

flags:
  --kind K                    object (default), link, action, trait or scheme
  --filter field.op=value     keep what matches, such as layer.equals=service; may repeat
  --limit N                   rows per page
  --all                       follow every page

example:
  praxium types list --filter layer.equals=service --limit 10
`},
	"types get": {
		usage: "usage: praxium types get [--kind K] [--include a,b] <id-or-name>",
		body: `Shows one type by its id (such as ot.site) or its name (such as Rack).

flags:
  --kind K        object (default), link, action, trait or scheme
  --include a,b   add parts to the answer, such as effective_properties,ancestors

example:
  praxium types get Rack --include effective_properties
`},
	"catalog": {
		usage: "usage: praxium catalog [link-meanings|channels|error-codes|reason-codes|roles|value-types|layers]",
		body: `Shows a catalog of the API. Without a name it shows the list of catalogs.

example:
  praxium catalog layers
`},
	"kg": {
		usage: "usage: praxium kg list|get ...",
		body: `The knowledge graphs of the server.
  list   list KGs (--role, --state, --limit, --all)
  get    show one KG (default main)
Run "praxium kg list --help" or "praxium kg get --help".
`},
	"kg list": {
		usage: "usage: praxium kg list [--role R] [--state S] [--limit N] [--all]",
		body: `Lists the KGs of the server.

flags:
  --role R     keep KGs of this role, such as production
  --state S    keep KGs in this state, such as serving
  --limit N    rows per page
  --all        follow every page
`},
	"kg get": {
		usage: "usage: praxium kg get [KG]",
		body: `Shows one KG by name or id. Without a name it shows main, the production KG.

example:
  praxium kg get main
`},
	"object": {
		usage: "usage: praxium object get|links ...",
		body: `One object of a KG, by its id (the ids are in the output of "query find").
  get     show the object
  links   show its links and neighbors
Run "praxium object get --help" or "praxium object links --help".
`},
	"object get": {
		usage: "usage: praxium object get [--kg main] [--include a,b] [read flags] <object-id>",
		body: `Shows one object: its type, name, attributes and state.

flags:
  --include a,b   add parts to the answer

` + readFlagsHelp},
	"object links": {
		usage: "usage: praxium object links [--kg main] [--direction in|out|both] [--link-type T]... [--steps N] [read flags] <object-id>",
		body: `Shows the links of an object and the objects on their other end.

flags:
  --direction D    in, out or both (default both)
  --link-type T    only this link type; may repeat
  --steps N        walk this many links away (default 1)

` + readFlagsHelp},
	"query": {
		usage: "usage: praxium query find|get|neighbors|impact ...",
		body: `Questions to a KG.
  find        find objects by type, trait, text, identifier or attribute
  get         get one object by id or identifier
  neighbors   walk the links around an object
  impact      what a failure reaches, or what an object depends on
Run "praxium query find --help" and so on.
`},
	"query find": {
		usage: "usage: praxium query find [--kg main] [--type T] [--no-subtypes] [--trait T] [--text X] [--identifier scheme:value] [--attr name.op=value]... [read flags]",
		body: `Finds objects. With no condition it lists the objects of the KG.

flags:
  --type T                   only objects of this type and its subtypes, such as PhysicalServer
  --no-subtypes              only this exact type
  --trait T                  only objects of types with this trait
  --text X                   objects whose name contains X
  --identifier scheme:value  the object that holds this identifier
  --attr name.op=value       a condition on an attribute (op: equals, in, prefix, range); may repeat.
                             in takes a comma list, range takes from..to

example:
  praxium query find --type PhysicalServer --attr power_state.equals=on --limit 5

` + readFlagsHelp},
	"query get": {
		usage: "usage: praxium query get [--kg main] [--include a,b] [read flags] <object-id> | --identifier scheme:value",
		body: `Gets one object by its id, or by an identifier it holds.

flags:
  --include a,b              add parts to the answer
  --identifier scheme:value  find the object by identifier instead of by id

` + readFlagsHelp},
	"query neighbors": {
		usage: "usage: praxium query neighbors [--kg main] [--direction D] [--link-type T]... [--meaning M]... [--peer-type T]... [--steps N] [read flags] <object-id>",
		body: `Walks the links around an object.

flags:
  --direction D   in, out or both
  --link-type T   only this link type; may repeat
  --meaning M     only links with this meaning; may repeat
  --peer-type T   only objects of this type on the other end; may repeat
  --steps N       walk this many links away

` + readFlagsHelp},
	"query impact": {
		usage: "usage: praxium query impact [--kg main] [--dead ID]... [--dead-link ID]... [--site-failure ID]... [--upstream ID] [--mode M] [--max-hops N] [--max-items N] [--time-budget PT30S] [read flags] [ID...]",
		body: `Asks what a failure reaches (downstream), or what an object depends on (--upstream). Failed objects may be given as
plain ids. The answer is the difference from the state with nothing failed.

flags:
  --dead ID            an object that has failed; may repeat (plain ids mean the same)
  --dead-link ID       a link that is cut; may repeat
  --site-failure ID    an object that fails together with everything contained in it; may repeat
  --upstream ID        list what this object depends on, and which of them alone would hurt it
  --mode M             facts_only, with_assumptions or both (default both)
  --max-hops N         stop the spread after N rounds
  --max-items N        keep at most N items per reading (default 5000)
  --time-budget D      stop after this long, such as PT10S (default PT30S)

how to read the output:
  summary      the count of objects per result; then "channel-empty objects": objects whose type needs a channel
               (power, cooling, ...) that no link supplies. They are listed, not counted as unknown
  results      down: lost; interrupted: lost for a while; unknown: cannot be told from the evidence;
               exposed_direct / exposed_inherited: a redundancy group lost its slack; degraded: partly lost
  readings     facts only believes observed or declared facts; with assumptions also believes assumed links.
               An object hit only with assumptions is marked assumption-based
  path         the chain of links that carried the failure to the object
  computation  complete, or the limit that stopped it (time_budget, max_hops); a stopped answer is a lower bound

examples:
  praxium query impact obj_1a2b...                            what breaks when this PDU fails
  praxium query impact --site-failure obj_9f... --mode facts_only --limit 50
  praxium query impact --upstream obj_77...                   what this server depends on

` + readFlagsHelp},
}

// Help is the help text of a client command, found by its words ("kg", "kg list"), or false when there is none.
func Help(path []string) (string, bool) {
	entry, ok := helpEntries[strings.Join(path, " ")]
	if !ok {
		return "", false
	}
	return entry.usage + "\n\n" + entry.body + "\n" + globalFlagsHelp, true
}

// UsageLine is the one usage line of a client command, for the message of a wrong command line.
func UsageLine(path []string) string {
	for n := len(path); n > 0; n-- {
		if entry, ok := helpEntries[strings.Join(path[:n], " ")]; ok {
			return entry.usage
		}
	}
	return "usage: praxium <command> ..."
}

// isHelpFlag says whether a word asks for help.
func isHelpFlag(word string) bool { return word == "-h" || word == "--help" || word == "-help" }
