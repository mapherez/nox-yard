package managed

import "github.com/mapherez/nox-yard/internal/store"

// A source sync changes source, not every saved deployment override. Keep
// declared values/files unless the request explicitly supplies a replacement.
// Empty strings remain explicit overrides, distinct from omitted fields.
func syncInputs(previous store.ManagedProject, source string, variables, files map[string]string) (map[string]string, map[string]string, error) {
	savedVariables, savedFiles, err := projectEnvironment(previous)
	if err != nil {
		return nil, nil, err
	}
	info, err := InspectSource(source)
	if err != nil {
		return nil, nil, err
	}
	nextVariables, nextFiles := map[string]string{}, map[string]string{}
	for _, variable := range Variables(source) {
		if value, ok := savedVariables[variable.Name]; ok && !envFileDefines(files[".env"], variable.Name) {
			nextVariables[variable.Name] = value
		}
	}
	for _, file := range info.EnvFiles {
		if value, ok := savedFiles[file.Path]; ok {
			nextFiles[file.Path] = value
		}
	}
	for key, value := range variables {
		nextVariables[key] = value
	}
	for key, value := range files {
		nextFiles[key] = value
	}
	return nextVariables, nextFiles, nil
}
