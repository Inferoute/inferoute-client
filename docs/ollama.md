##Running Ollama on port 0.0.0.0


### Linux 
Create a override.conf file for the Ollama System Service

```bash
nano /etc/systemd/system/ollama.service.d/override.conf
```

Add the following line to the file and save

```bash
Environment="OLLAMA_HOST=0.0.0.0:11434"
```


### MAC OSX

#### Installed as application

 1. Set a Envrionment Variable

`export OLLAMA_HOST=http://0.0.0.0:11434`

    - For permanent effect - add this line to `.zshrc or .bashrc` file

2. Then stop and restart Ollama

Another solution that has been suggested:

You have to use `launchctl setenv OLLAMA_HOST 0.0.0.0:11434` and restart ollama and the terminal.
https://stackoverflow.com/questions/603785/environment-variables-in-mac-os-x

#### Installed via Homebrew

1. update the homebrew.mxcl.ollama.plist:
`nano /opt/homebrew/opt/ollama/homebrew.mxcl.ollama.plist`

2. Add the environment variable lines in plist format:
```
<key>EnvironmentVariables</key>
<dict>
    <key>OLLAMA_HOST</key>
    <string>0.0.0.0</string>
</dict>
```

3. Restart ollama

`brew services restart ollama`



### Windows




