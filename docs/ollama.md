### Linux - Running Ollama on port 0.0.0.0


Create a override.conf file for the Ollama System Service

```bash
nano /etc/systemd/system/ollama.service.d/override.conf
```

Add the following line to the file and save

```bash
Environment="OLLAMA_HOST=0.0.0.0:11434"
```


