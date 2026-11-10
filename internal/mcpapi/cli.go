package mcpapi

import (
	"context"
	"strings"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Presentation belongs to negotiated NoX discovery, never ordinary tools/list.
// References invoke the existing tools with their unchanged schemas and guards.
func cliDiscovery(tools []noxmcp.Tool) noxmcp.DiscoveryOptions {
	return noxmcp.DiscoveryOptions{
		ServerID: "nox-yard", Descriptors: cliDescriptors(tools),
		ResolveView: func(context.Context, *mcp.RequestExtra, []*mcp.Tool) (string, []string, []string, noxmcp.JSON, error) {
			return "suite", nil, nil, nil, nil
		},
	}
}

func cliDescriptors(tools []noxmcp.Tool) map[string]noxmcp.JSON {
	groups := map[string]string{"container": "Containers", "container log": "Logs", "project": "Projects", "project configuration": "Configuration", "project override": "Overrides", "project schedule": "Daily updates", "compose": "Compose", "recreate": "Recreate", "job": "Jobs", "settings projects": "Projects", "update": "Yard updates"}
	categories := map[string]int{"health": 10, "status": 20, "metrics": 30, "project": 100, "container": 200, "compose": 300, "recreate": 400, "job": 500, "settings": 600, "update": 700}
	ranks := map[string]int{"list": 1, "get": 1, "status": 1, "inspect": 1, "read": 1, "preview": 2, "history": 3, "logs": 4, "env": 5, "action": 10, "pull": 20, "set": 30, "save": 30, "apply": 40, "submit": 40, "remove": 90}
	descriptors := map[string]noxmcp.JSON{}
	for _, tool := range tools {
		path := strings.Fields(cliPaths[tool.Name])
		nav := noxmcp.JSON{"label": tool.Title, "order": categories[path[0]] + ranks[path[len(path)-1]]}
		if len(path) > 1 {
			nav["group"] = groups[strings.Join(path[:len(path)-1], " ")]
		}
		params := noxmcp.JSON{}
		for name, property := range tool.InputSchema.Properties {
			if property.Ref == "" && len(property.Properties) == 0 {
				order := 50
				if name == "id" || name == "target" || name == "name" {
					order = 10
				}
				if name == "action" || name == "operation" || name == "mode" {
					order = 20
				}
				params["/"+name] = noxmcp.JSON{"order": order}
			}
		}
		descriptors[tool.Name] = noxmcp.JSON{"version": "1.0", "navigation": nav, "parameters": params}
	}
	parameters := func(name string) noxmcp.JSON {
		d := descriptors[name]
		if p, ok := d["parameters"].(map[string]any); ok {
			return p
		}
		p := noxmcp.JSON{}
		d["parameters"] = p
		return p
	}
	field := func(name, path, control string, order int) {
		parameters(name)[path] = noxmcp.JSON{"control": control, "order": order}
	}
	require := func(name string, features ...string) {
		old, _ := descriptors[name]["requires"].([]string)
		for _, feature := range features {
			found := false
			for _, v := range old {
				if v == feature {
					found = true
				}
			}
			if !found {
				old = append(old, feature)
			}
		}
		descriptors[name]["requires"] = old
	}
	confirm := func(name, message, match, bind string) {
		c := noxmcp.JSON{"mode": "boolean", "message": message}
		if match != "" {
			c["mode"] = "typed"
			c["matchPointer"] = match
		}
		if bind != "" {
			c["bindPointer"] = bind
		}
		descriptors[name]["confirmation"] = c
		require(name, "confirmation")
	}
	source := func(tool string, inputs ...string) noxmcp.JSON { return noxmcp.JSON{"tool": tool, "input": inputs} }
	preview := func(name, tool string, inputs []string, bind noxmcp.JSON) {
		p := source(tool, inputs...)
		p["bind"] = bind
		descriptors[name]["preview"] = p
		require(name, "preview")
	}
	// The existing projects array contains opaque project IDs. Container inventories
	// are nested; those IDs remain explicit text inputs, not guessed projections.
	for _, name := range []string{"yard_project_action", "yard_project_pull", "yard_project_remove_preview", "yard_project_remove", "yard_project_schedule_get", "yard_project_schedule_set", "yard_recreate_preview", "yard_recreate_submit", "yard_job_history"} {
		pointer := "/id"
		if name == "yard_job_history" {
			pointer = "/target"
		}
		parameters(name)[pointer] = noxmcp.JSON{"order": 10, "options": noxmcp.JSON{"tool": "yard_projects", "arguments": noxmcp.JSON{}, "itemsPointer": "/projects", "valuePointer": "/id", "labelPointer": "/name"}}
		require(name, "dynamic-options")
	}
	for _, tool := range tools {
		if !tool.ReadOnly {
			confirm(tool.Name, "Run the reviewed Yard operation? Accepted jobs must be checked for completion.", "", "")
		}
	}
	for _, kind := range []string{"container", "project"} {
		name := "yard_" + kind + "_remove"
		preview(name, name+"_preview", []string{"/id", "/removeVolumes", "/removeImages"}, noxmcp.JSON{"/fingerprint": "/fingerprint"})
		confirm(name, "Remove the selected target and the reviewed resources? Volume/image removal follows the selected choices. Type the target ID to confirm.", "/id", "/confirm")
	}
	preview("yard_recreate_submit", "yard_recreate_preview", []string{"/id", "/operation", "/service"}, noxmcp.JSON{"/fingerprint": "/fingerprint"})
	confirm("yard_recreate_submit", "Recreate the reviewed scope? Containers may be replaced or interrupted; application data writes are not rolled back. Type the target ID to confirm.", "/id", "/confirm")
	preview("yard_compose_submit", "yard_compose_preview", []string{"/name", "/source", "/variables", "/envFiles", "/mode", "/profiles", "/interpolationFiles", "/projectDir"}, noxmcp.JSON{"/fingerprint": "/fingerprint"})
	confirm("yard_compose_submit", "Submit the reviewed Compose deployment or adoption? Type the project name to confirm. Submission returns a job, not completed deployment.", "/name", "")
	confirm("yard_compose_operation", "Run the selected managed Compose operation with these resource-removal choices? Type the project name to confirm.", "/name", "")
	preview("yard_job_recovery_acknowledge", "yard_job", []string{"/id"}, noxmcp.JSON{"/updatedAt": "/updatedAt"})
	confirm("yard_job_recovery_acknowledge", "Confirm only after inspecting the target and retained resources on the host. This releases the reviewed reservation; it does not retry or restore the operation. Type the job ID.", "/id", "/confirm")
	confirm("yard_self_update_check_and_update", "Check for and install a newer Yard image now? Yard may restart even when automatic updates are disabled.", "", "")
	field("yard_project_variable_set", "/value", "password", 40)
	field("yard_project_configuration_save", "/content", "textarea", 40)
	field("yard_projects_settings_set", "/projectsBase", "text", 10)
	for _, name := range []string{"yard_compose_source", "yard_compose_preview", "yard_compose_submit", "yard_project_override_preview", "yard_project_override_apply"} {
		prefix := "/source"
		if name == "yard_compose_source" {
			prefix = ""
		}
		field(name, prefix+"/kind", "text", 20)
		field(name, prefix+"/url", "text", 30)
		field(name, prefix+"/filename", "text", 40)
		field(name, prefix+"/yaml", "textarea", 50)
		if name != "yard_compose_source" {
			field(name, "/variables", "password", 60)
			field(name, "/envFiles", "password", 70)
		}
	}
	// Overrides use /files, not the managed deployment's /envFiles.
	for _, name := range []string{"yard_project_override_preview", "yard_project_override_apply"} {
		delete(parameters(name), "/envFiles")
		field(name, "/files", "password", 70)
	}
	parameters("yard_project_configuration_save")["/path"] = noxmcp.JSON{"order": 20, "options": noxmcp.JSON{"tool": "yard_project_configuration_read", "arguments": noxmcp.JSON{"id": cliInput("/id")}, "itemsPointer": "/files", "valuePointer": "/path", "labelPointer": "/path"}}
	require("yard_project_configuration_save", "dynamic-options")
	// Take the optimistic-concurrency revision before the human edits. Rebinding a
	// newer fingerprint after editing could overwrite an unreviewed file change.
	for _, name := range []string{"yard_project_configuration_save", "yard_project_variable_set", "yard_project_override_preview", "yard_project_override_apply"} {
		var inputs []string
		switch name {
		case "yard_project_configuration_save":
			inputs = []string{"/path", "/content", "/profiles", "/interpolationFiles"}
		case "yard_project_variable_set":
			inputs = []string{"/service", "/name", "/value"}
		default:
			inputs = []string{"/action", "/path", "/source/kind", "/source/url", "/source/filename", "/source/yaml", "/variables", "/files"}
		}
		writeArgs := noxmcp.JSON{"id": cliInput("/id"), "fingerprint": cliStep("configuration", "/fingerprint")}
		for _, p := range inputs {
			if strings.HasPrefix(p, "/source/") {
				writeArgs["source"] = cliInput("/source")
			} else {
				writeArgs[strings.TrimPrefix(p, "/")] = cliInput(p)
			}
		}
		steps := []any{cliInputStep("target", "/id"), cliCallStep("configuration", "yard_project_configuration_read", noxmcp.JSON{"id": cliInput("/id")}), cliInputStep("edit", inputs...)}
		if strings.HasPrefix(name, "yard_project_override_") {
			steps = append(steps, cliCallStep("preview", "yard_project_override_preview", writeArgs))
			if name == "yard_project_override_apply" {
				args := noxmcp.JSON{"id": cliInput("/id")}
				for _, key := range []string{"action", "path", "source", "variables", "files", "fingerprint", "candidateFingerprint"} {
					args[key] = cliStep("preview", "/"+key)
				}
				steps = append(steps, cliConfirmStep("review", "Save the reviewed override selection and files? Containers are not changed.", "/id"), cliCallStep("save", name, args))
			}
		} else {
			steps = append(steps, cliConfirmStep("review", "Save the reviewed project configuration? This does not recreate containers.", "/id"), cliCallStep("save", name, writeArgs))
		}
		delete(descriptors[name], "confirmation")
		delete(descriptors[name], "preview")
		descriptors[name]["workflow"] = steps
		require(name, "workflow")
	}
	return descriptors
}
func cliInput(path string) noxmcp.JSON { return noxmcp.JSON{"from": "input", "pointer": path} }
func cliStep(step, path string) noxmcp.JSON {
	return noxmcp.JSON{"from": "step", "step": step, "pointer": path}
}
func cliInputStep(id string, paths ...string) noxmcp.JSON {
	return noxmcp.JSON{"id": id, "type": "input", "parameters": paths}
}
func cliCallStep(id, tool string, args noxmcp.JSON) noxmcp.JSON {
	return noxmcp.JSON{"id": id, "type": "call", "tool": tool, "arguments": args}
}
func cliConfirmStep(id, message, path string) noxmcp.JSON {
	return noxmcp.JSON{"id": id, "type": "confirm", "confirmation": noxmcp.JSON{"mode": "typed", "message": message + " Type the target ID to confirm.", "matchPointer": path}}
}
