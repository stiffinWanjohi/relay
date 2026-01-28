package main

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
)

//go:embed openapi.yaml
var openapiSpec embed.FS

func cmdOpenAPI(args []string) error {
	if len(args) == 0 {
		return cmdOpenAPIPrint()
	}

	switch args[0] {
	case "serve":
		port := "8081"
		if len(args) > 1 {
			port = args[1]
		}
		return cmdOpenAPIServe(port)
	case "print", "show":
		return cmdOpenAPIPrint()
	default:
		return fmt.Errorf("unknown openapi subcommand: %s (use 'serve' or 'print')", args[0])
	}
}

func cmdOpenAPIPrint() error {
	spec, err := getOpenAPISpec()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(spec)
	return err
}

func cmdOpenAPIServe(port string) error {
	spec, err := getOpenAPISpec()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(spec)
	})

	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.Error(w, "JSON format not yet supported, use /openapi.yaml", http.StatusNotImplemented)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, swaggerUIHTML(port))
	})

	fmt.Printf("Serving OpenAPI spec at http://localhost:%s\n", port)
	fmt.Printf("  Swagger UI: http://localhost:%s/\n", port)
	fmt.Printf("  Raw spec:   http://localhost:%s/openapi.yaml\n", port)
	return http.ListenAndServe(":"+port, mux)
}

func getOpenAPISpec() ([]byte, error) {
	return openapiSpec.ReadFile("openapi.yaml")
}

func swaggerUIHTML(port string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>Relay API Documentation</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: "http://localhost:%s/openapi.yaml",
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        layout: "BaseLayout"
      });
    };
  </script>
</body>
</html>`, port)
}
