# HTTPS 证书目录

请把生产证书文件放到本目录，并统一命名为：

- `tls.crt`：证书链文件（如 `fullchain.pem` / `bundle.pem`）
- `tls.key`：私钥文件
- `git.lecspace.com_bundle.pem`：Gitea 域名 `git.lecspace.com` 的证书链
- `git.lecspace.com.key`：Gitea 域名 `git.lecspace.com` 的私钥

`docker-compose.full.yml` 会把本目录挂载到前端容器的 `/etc/nginx/certs`，
`frontend/nginx.conf` 与 `deploy/nginx/default.conf` 也会固定从该路径读取这些证书文件。

注意：

1. 证书和私钥属于敏感文件，不要提交到仓库。
2. 若证书文件名不同，请在部署前重命名为上面的通用名称。
3. 更新证书后，只需要重建或重启 `frontend` 容器即可生效。
4. 如果暂时不接入 Gitea 域名，可先不放 `git.lecspace.com_bundle.pem` 与 `git.lecspace.com.key`，但对应站点会无法完成 HTTPS 代理。
