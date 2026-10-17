# timesheet-cli

Cliente de linha de comando multiplataforma para gerenciar lançamentos e consultar faturas no Timesheet da [Luby](https://github.com/lubysoftware). Ele se autentica diretamente nos endpoints HTTP do site; depois da instalação, não precisa de navegador nem de runtime de linguagem.

## Instalação

No macOS ou no Linux, instale a release mais recente em `/usr/local/bin` com:

```sh
curl -fsSL https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.sh | sh
```

O script detecta o sistema operacional e a arquitetura e verifica o checksum da release. No Windows, use o instalador em PowerShell; ele instala em `%LOCALAPPDATA%\Programs\timesheet` e adiciona a pasta ao `PATH` do seu usuário:

```powershell
irm https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.ps1 | iex
```

Os dois instaladores terminam com perguntas opcionais e separadas sobre a skill de agente incluída (`skills/timesheet-cli/SKILL.md`): uma para o diretório global de agentes (`~/.agents/skills`) e outra para o Claude (`~/.claude/skills`). Responda `y` para instalar a skill naquele local ou pressione Enter para pular.

Verifique a instalação:

```sh
timesheet --version
timesheet --help
```

## Atualização

```sh
timesheet upgrade
```

O comando baixa a release mais recente para a sua plataforma, verifica o checksum e substitui o executável em uso. No Linux, ele também reinicia o [serviço MCP](#rodando-como-serviço-no-linux) se ele estiver em execução.

## Primeiros passos

Faça login uma vez e depois gerencie seus lançamentos:

```sh
timesheet login

timesheet add \
  --customer ernst \
  --project internal \
  --category daily \
  --start 09:00 \
  --end 10:00 \
  --desc "Reunião diária."

timesheet list
```

A sessão fica em `~/.timesheet-cli/session.json`. Para que a CLI se autentique de novo quando o servidor expirar a sessão, salve suas credenciais no cofre de credenciais do sistema operacional:

```sh
timesheet login --save-credentials
timesheet logout                  # mantém as credenciais salvas
timesheet logout --forget-credentials
```

Isso usa o Keychain no macOS, o Gerenciador de Credenciais no Windows ou a API Secret Service no Linux. Em um Linux sem interface gráfica e sem Secret Service, use `timesheet login --save-credentials --credential-store file`; as credenciais ficam então em `~/.timesheet-cli/credentials.json`, com permissão `0600`.

## Comandos

```text
timesheet login [--user LOGIN] [--pass SENHA] [--save-credentials] [--credential-store system|file]
timesheet logout [--forget-credentials]
timesheet list [--limit N] [--all]
timesheet meta
timesheet status ID_DO_LANÇAMENTO
timesheet show ID_DO_LANÇAMENTO
timesheet invoice list [--limit N] [--offset N] [--search TEXTO] [--status pending|sent] [--all]
timesheet invoice preview ID_DO_PROCESSO
timesheet add --customer CLIENTE --project PROJETO --category CATEGORIA \
  [--date DATA] --start HH:mm --end HH:mm --desc DESCRIÇÃO [--not-monetize]
timesheet update ID_DO_LANÇAMENTO [flags do lançamento] [--monetize | --not-monetize]
timesheet delete ID_DO_LANÇAMENTO [--yes]
timesheet mcp [--http ENDEREÇO] [--token-file CAMINHO]
timesheet mcp install [--http ENDEREÇO] [--token-file CAMINHO]
timesheet mcp uninstall
timesheet upgrade
timesheet version
```

Todos os comandos aceitam `-h`/`--help`. `--json` é uma flag global e pode vir antes ou depois do subcomando.

Cliente, projeto e categoria aceitam um ID numérico ou um trecho único do nome, sem diferenciar maiúsculas de minúsculas. Datas aceitam `YYYY-MM-DD` ou `dd/MM/yyyy` e, se omitidas, assumem o dia de hoje. Horários usam o formato 24 horas `HH:mm`.

Exemplos:

```sh
timesheet list --limit 5
timesheet meta
timesheet status 12345
timesheet invoice list --status pending
timesheet invoice preview 901
timesheet update 12345 --end 18:00 --monetize
timesheet delete 12345 --yes
```

`delete` pede confirmação, a menos que `--yes` seja informado. `timesheet update ID_DO_LANÇAMENTO` altera um lançamento, enquanto `timesheet upgrade` atualiza a própria CLI.

## Saída JSON

Com `--json`, os comandos bem-sucedidos imprimem um envelope no stdout:

```json
{"ok":true,"data":{}}
```

As falhas imprimem um envelope no stderr e terminam com código diferente de zero:

```json
{"ok":false,"error":{"code":"invalid_input","message":"..."}}
```

Os códigos de erro estáveis são `usage`, `invalid_input`, `auth_required`, `login_failed`, `credential_store_error`, `not_found`, `ambiguous_value`, `network_error`, `invalid_server_response`, `operation_failed`, `upgrade_failed` e `internal_error`.

## Servidor MCP

`timesheet mcp` oferece as operações da CLI a agentes de IA pelo [Model Context Protocol](https://modelcontextprotocol.io). O agente recebe ferramentas; a sessão e as credenciais salvas ficam com a CLI e nunca chegam até ele.

| Ferramenta | Comando equivalente |
| --- | --- |
| `list_entries` | `timesheet list` |
| `get_metadata` | `timesheet meta` |
| `get_entry` | `timesheet show` |
| `get_entry_status` | `timesheet status` |
| `list_invoices` | `timesheet invoice list` |
| `preview_invoice` | `timesheet invoice preview` |
| `create_entry` | `timesheet add` |
| `update_entry` | `timesheet update` |
| `delete_entry` | `timesheet delete --yes` |

As ferramentas de leitura são marcadas como somente leitura e as de escrita como destrutivas, para que clientes que pedem aprovação a cada chamada possam liberar as leituras e continuar pedindo aprovação para as escritas. Os resultados usam o mesmo envelope do `--json`. `upgrade`, `login` e `logout` não são expostos. Faça login uma vez com `timesheet login --save-credentials` (adicione `--credential-store file` em uma máquina sem interface gráfica) para que o servidor consiga renovar a sessão.

Sem flags, o servidor fala MCP pelo stdin e stdout, para clientes que o iniciam como processo local:

```json
{ "mcpServers": { "timesheet": { "command": "timesheet", "args": ["mcp"] } } }
```

Com `--http`, ele serve Streamable HTTP em `/mcp`:

```sh
timesheet mcp --http 127.0.0.1:8765
```

Toda requisição HTTP precisa enviar `Authorization: Bearer <token>`. O token é lido de `TIMESHEET_MCP_TOKEN` ou de `~/.timesheet-cli/mcp-token`, que é criado com um token aleatório (permissão `0600`) na primeira execução; `--token-file` indica outro arquivo, que precisa já existir. O token deve ter pelo menos 32 caracteres.

### Rodando como serviço no Linux

Para manter o servidor HTTP rodando em segundo plano, instale-o como serviço de usuário do systemd:

```sh
timesheet login --save-credentials --credential-store file
timesheet mcp install                 # escuta em 127.0.0.1:8765; --http muda o endereço
loginctl enable-linger "$USER"        # mantém o serviço rodando com você deslogado
```

O `install` grava `~/.config/systemd/user/timesheet-mcp.service` apontando para o executável em uso, cria o arquivo de token se necessário e habilita e inicia o serviço, que passa a subir de novo a cada login e depois de uma falha. Rodar o comando outra vez substitui o serviço e o reinicia. `TIMESHEET_BASE_URL`, `TIMESHEET_SESSION` e `TIMESHEET_CREDENTIALS` são copiadas para o serviço quando definidas; o serviço sempre lê o token do arquivo, nunca de `TIMESHEET_MCP_TOKEN`. O comando avisa quando o lingering está desligado, porque nesse caso o systemd para o serviço quando sua última sessão termina.

Em um servidor, use o armazenamento de credenciais em arquivo: o cofre do sistema operacional costuma ficar bloqueado ou indisponível enquanto você está deslogado, e o serviço não conseguiria renovar a sessão. O `timesheet upgrade` reinicia o serviço quando ele está rodando, para que carregue a nova versão; com `sudo`, reinicie você mesmo com `systemctl --user restart timesheet-mcp`. Acompanhe o serviço com `systemctl --user status timesheet-mcp` e `journalctl --user -u timesheet-mcp`, e remova-o com `timesheet mcp uninstall`, que mantém o arquivo de token, a sessão e as credenciais.

O serviço funciona só no Linux. No macOS ou no Windows, rode `timesheet mcp --http ENDEREÇO` pelo launchd, pelo Agendador de Tarefas ou por outro gerenciador de serviços.

### Conectando por HTTPS

Clientes que exigem HTTPS, como os servidores MCP remotos do Wisp Bot, podem acessá-lo por um proxy reverso com TLS. Em uma máquina de uma tailnet do Tailscale:

```sh
tailscale serve --bg --https=8443 http://127.0.0.1:8765
```

Depois, adicione `https://<máquina>.<tailnet>.ts.net:8443/mcp` como servidor MCP remoto com autenticação por header: header `Authorization`, valor `Bearer <token>`.

## Configuração

| Variável | Finalidade | Padrão |
| --- | --- | --- |
| `TIMESHEET_USER` | Usuário de login | Pergunta interativa |
| `TIMESHEET_PASS` | Senha de login | Pergunta interativa |
| `TIMESHEET_SESSION` | Local do arquivo de sessão | `~/.timesheet-cli/session.json` |
| `TIMESHEET_CREDENTIALS` | Local do armazenamento de credenciais em arquivo | `~/.timesheet-cli/credentials.json` |
| `TIMESHEET_BASE_URL` | URL do servidor do Timesheet | `https://luby-timesheet.azurewebsites.net` |
| `TIMESHEET_MCP_TOKEN` | Token bearer do `timesheet mcp --http` | `~/.timesheet-cli/mcp-token` |

Os arquivos de sessão, de credenciais e de token do MCP contêm segredos de autenticação. Não os imprima, compartilhe nem faça commit deles.
