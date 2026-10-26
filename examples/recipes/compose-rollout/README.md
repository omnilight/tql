# Roll out a labelled group of Compose services

Update both frontend image tags and log rotation limits in one transaction.
Selecting services by the `com.example.rollout` label avoids maintaining a list
of service names. The Redis service and all port mappings remain untouched;
`admin` gains the missing `max-file` option in the local mapping style.

This recipe assumes selected services already use the `json-file` logging driver
and have a `logging.options` mapping. The example image tags illustrate a rollout;
choose and test image versions appropriate for your deployment. The script edits
the Compose file and does not start or deploy containers.

From the repository root, preview the edit:

```sh
tql --query-file examples/recipes/compose-rollout/migrate.tql \
  examples/recipes/compose-rollout/compose.yaml
```

Apply it to your own file after adapting the script:

```sh
tql -i --query-file examples/recipes/compose-rollout/migrate.tql compose.yaml
```

The complete [input](compose.yaml), [script](migrate.tql), and
[expected output](expected/compose.yaml) are checked in. The script is idempotent:
running it again produces no changes. These exact bytes and a second run are
verified by `go test ./...`.

```diff
--- compose.yaml
+++ expected/compose.yaml
@@ -3,5 +3,5 @@
 services:
   web:
-    image: nginx:1.27-alpine # Updated together with the admin frontend.
+    image: "nginx:1.28-alpine" # Updated together with the admin frontend.
     labels:
       com.example.rollout: frontend
@@ -11,8 +11,8 @@
       driver: json-file
       options:
-        max-size: "10m"
-        max-file: "2"
+        max-size: "20m"
+        max-file: "3"
   admin:
-    image: nginx:1.27-alpine
+    image: "nginx:1.28-alpine"
     labels:
       com.example.rollout: frontend
@@ -22,5 +22,6 @@
       driver: json-file
       options:
-        max-size: "10m"
+        max-size: "20m"
+        max-file: "3"
   cache:
     image: redis:7-alpine # This unrelated service must remain untouched.
```

String values being replaced may receive quotes; other source ranges retain
their original spelling. [Field reference](https://docs.docker.com/reference/compose-file/services/): Compose service configuration.

[All recipes](../README.md)
