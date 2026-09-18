# Kubernetes Mutating Webhook 学习示例

一个用 Go 编写的 Kubernetes Admission Webhook。收到 Pod 创建请求后，服务返回 JSON Patch，为 Pod 写入 annotation：

```yaml
metadata:
  annotations:
    annotation-injected-by: webhook
```

本项目用于理解 `AdmissionReview` 请求、响应和 JSON Patch 的工作流程。当前实现会替换整个 `metadata.annotations`，已有 annotation 会被覆盖，适合在独立测试集群中学习使用。

## 请求流程

```text
kube-apiserver
  └─ HTTPS :443 → ALB（终止 TLS）
                   └─ HTTP :80 → Service → webhook-server
                                            └─ AdmissionReview + JSON Patch
```

程序监听 HTTP `:80`，`POST /` 处理 `admission.k8s.io/v1` 请求，`GET /healthz` 返回 HTTP 200，供负载均衡器检查存活状态。程序本身不加载 TLS 证书。部署示例由 AWS ALB 提供 HTTPS 入口。

## 目录结构

```text
.
├── main.go                               # HTTP 入口
├── pkg/webserv.go                        # 请求解码、注解补丁、响应
├── examples/admission-review.json        # 合成的 Pod 创建请求
├── deploy/
│   ├── webhook-server-deployment.yaml   # Webhook 工作负载
│   ├── webhook-server-service.yaml      # 集群内 HTTP Service
│   ├── webhook-server-ingress.yaml      # AWS ALB HTTPS 入口
│   ├── MutatingWebhookConfiguration.yaml
│   └── nginx-deployment.yaml            # 测试工作负载
├── Dockerfile
├── .dockerignore
└── .gitignore
```

## 本地运行

需要 Go 1.24.4 或更高版本，以及 `curl`。使用 Docker 可将容器的 80 端口映射到本机 8080，避免本机低端口权限问题。

```bash
go mod download
go test ./...
go build -o bin/webhook-server .
./bin/webhook-server
```

程序固定监听 80 端口；直接运行时需要该端口空闲，并具备绑定低端口的权限。也可以用 Docker：

```bash
docker build -t webhook-server:local .
docker run --rm --name webhook-server -p 127.0.0.1:8080:80 webhook-server:local
```

在另一个终端发送示例请求（直接运行二进制时将端口改为 `80`）：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -sS http://127.0.0.1:8080/ \
  -H 'Content-Type: application/json' \
  --data-binary @examples/admission-review.json
```

响应中的 `response.uid` 应与请求一致，`allowed` 为 `true`，`patchType` 为 `JSONPatch`。`patch` 是 Base64 编码的补丁，可用 Python 3 解码查看：

```bash
curl -sS http://127.0.0.1:8080/ \
  -H 'Content-Type: application/json' \
  --data-binary @examples/admission-review.json \
  | python3 -c 'import base64,json,sys; print(base64.b64decode(json.load(sys.stdin)["response"]["patch"]).decode())'
```

预期补丁：

```json
[{"op":"add","path":"/metadata/annotations","value":{"annotation-injected-by":"webhook"}}]
```

## 部署到 Kubernetes（AWS ALB 示例）

### 前置条件和配置

需要可用的 Kubernetes 测试集群、`kubectl`、镜像仓库，以及已配置权限和网络的 AWS Load Balancer Controller。准备与 Webhook 域名匹配的 ACM 证书，并确保 API Server 能访问 ALB 的 HTTPS 入口。

先将示例复制到已被 Git 忽略的本地目录，再编辑实际配置：

```bash
mkdir -p deploy/local
cp deploy/*.yaml deploy/local/
```

| 文件 | 需要替换或检查的配置 |
| --- | --- |
| `webhook-server-deployment.yaml` | 将 `registry.example.com/demo/webhook-server:latest` 替换成实际镜像地址 |
| `webhook-server-ingress.yaml` | 替换 `webhook.example.com`、`<ACM_CERTIFICATE_ARN>`、`<SECURITY_GROUP_ID>`；按网络环境选择 ALB scheme |
| `MutatingWebhookConfiguration.yaml` | 将 URL 中的 `webhook.example.com` 替换为同一个 HTTPS 域名 |

占位符必须手动替换，`kubectl apply` 不会自动展开。Ingress 示例使用公网 ALB，安全组应根据 API Server 的实际网络来源限制访问。证书和 ALB 应位于匹配的区域。若使用私有 CA，还需在 Webhook 的 `clientConfig.caBundle` 中提供 PEM CA 证书的 Base64 内容；省略该字段时使用 API Server 的系统信任根。参见 [Kubernetes Webhook 配置](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) 和 [ALB Ingress 注解](https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/guide/ingress/annotations/)。

### 构建并部署服务

将下面的示例镜像地址改为自己有推送权限的仓库，完成仓库登录后执行：

```bash
export WEBHOOK_IMAGE='registry.example.com/demo/webhook-server:latest'
docker build -t "$WEBHOOK_IMAGE" .
docker push "$WEBHOOK_IMAGE"

kubectl apply -f deploy/local/webhook-server-deployment.yaml
kubectl apply -f deploy/local/webhook-server-service.yaml
kubectl apply -f deploy/local/webhook-server-ingress.yaml
kubectl rollout status deployment/webhook-server -n default
kubectl get ingress webhook-server -n default
```

镜像架构需与集群节点匹配；私有仓库还需要节点拉取权限或 `imagePullSecrets`。ALB 创建完成后，将域名解析到其入口，检查目标组健康状态，再用实际域名验证 HTTPS：

```bash
curl -i https://webhook.example.com/healthz
curl -sS https://webhook.example.com/ \
  -H 'Content-Type: application/json' \
  --data-binary @examples/admission-review.json
```

### 注册 Webhook 并验证

确认 HTTPS 请求能正常返回后再注册。清单匹配所有命名空间的 Pod `CREATE`，未设置 `failurePolicy` 时默认是 `Fail`：Webhook 不可用可能阻止集群创建 Pod，包括 Webhook 自身的替代副本。测试前应按需要增加 `namespaceSelector` 限定范围。具体语义见 [Kubernetes Admission Webhook 文档](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)。

```bash
kubectl apply -f deploy/local/MutatingWebhookConfiguration.yaml
kubectl apply -f deploy/local/nginx-deployment.yaml
kubectl rollout status deployment/nginx-test -n default
kubectl get pods -n default -l app=nginx-test \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.annotation-injected-by}{"\n"}{end}'
```

预期每个 Pod 名称后显示 `webhook`。已经存在的 Pod 不会自动触发创建准入，可通过 `kubectl rollout restart deployment/nginx-test -n default` 创建新副本验证。

### 清理

先注销 Webhook，避免删除服务后继续拦截 Pod 创建，再清理示例资源：

```bash
kubectl delete -f deploy/local/MutatingWebhookConfiguration.yaml --ignore-not-found
kubectl delete -f deploy/local/nginx-deployment.yaml --ignore-not-found
kubectl delete -f deploy/local/webhook-server-ingress.yaml --ignore-not-found
kubectl delete -f deploy/local/webhook-server-service.yaml --ignore-not-found
kubectl delete -f deploy/local/webhook-server-deployment.yaml --ignore-not-found
```

## 排查问题

- **`ImagePullBackOff`**：检查镜像地址、架构和仓库拉取权限。
- **ALB 目标不健康**：检查 `/healthz` 是否返回 200、Service selector 和安全组到 Pod 80 端口的连通性。
- **证书错误或请求超时**：检查 DNS、证书域名、CA 信任，以及 API Server 到 ALB 的网络路径。
- **HTTP 400 / 415**：请求必须包含非空的 AdmissionReview JSON，并使用 `Content-Type: application/json`。
- **Pod 创建失败**：查看 `kubectl describe deployment nginx-test -n default`、ReplicaSet 事件和 `kubectl logs -n default -l app=webhook-server`。必要时先删除 Webhook 配置恢复创建能力。

## 数据与凭据管理

示例域名、镜像地址和请求数据使用通用占位值。实际部署配置放在 `deploy/local/`；证书、私钥、环境文件和日志由 `.gitignore` 与 `.dockerignore` 排除。运行日志只记录处理状态。

历史版本曾包含测试私钥和原始请求记录。工作区脱敏和忽略规则不会清除已有 Git 历史；曾提交的私钥应视为已暴露，使用方需要轮换。公开仓库前应另行清理所有相关分支和标签的历史，并处理已有副本。当前实现保留了简化的请求校验、固定端口和注解覆盖逻辑，生产使用前需要补充相应处理。
