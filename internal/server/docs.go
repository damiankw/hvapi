package server

// docsHTML is the /docs page. It renders /openapi.json with Swagger UI,
// loaded from a CDN at a pinned version. The host itself needs no internet
// access; the browser opening the page does. The raw document at
// /openapi.json works without it.
const docsHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>hvapi API documentation</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css" crossorigin="anonymous">
  <style>
    body { margin: 0; }
    #fallback { font-family: sans-serif; margin: 2rem; display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <div id="fallback">
    <h1>hvapi API documentation</h1>
    <p>The documentation viewer could not be loaded from the CDN (this browser may be offline).
       The API description is still available as <a href="openapi.json">openapi.json</a>;
       open it in any OpenAPI viewer.</p>
  </div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" crossorigin="anonymous"></script>
  <script>
    if (window.SwaggerUIBundle) {
      SwaggerUIBundle({
        url: "openapi.json",
        dom_id: "#swagger-ui",
        persistAuthorization: true,
        tryItOutEnabled: true,
        displayRequestDuration: true,
        defaultModelsExpandDepth: 1
      });
    } else {
      document.getElementById("fallback").style.display = "block";
    }
  </script>
</body>
</html>
`
