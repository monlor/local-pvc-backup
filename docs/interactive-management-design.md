# PVC备份管理交互式方案设计

## 1. 架构概述

### 当前架构分析
- DaemonSet部署在每个节点上，每个实例管理本节点的PVC备份
- 基于restic + S3存储，每个节点有独立的repository：`s3:endpoint/bucket/path/node-{nodeName}`
- 通过Pod annotations配置备份策略
- 支持include/exclude模式和retention策略

### 新增交互式管理能力
在现有架构基础上增加CLI命令，支持跨节点的PVC备份管理。

## 2. 统一过滤参数设计

所有命令支持一致的过滤参数：

```bash
# 通用过滤参数
--node <node-name>              # 按节点过滤
--namespace <namespace>         # 按命名空间过滤  
--pvc <pvc-name>               # 按PVC名称过滤
--all                          # 所有启用备份的PVC
```

### 过滤参数组合规则
- `--all`: 选择所有启用备份的PVC
- `--node + --namespace`: 指定节点上指定命名空间的PVC
- `--node + --pvc`: 指定节点上指定PVC（需要同时指定namespace）
- `--namespace + --pvc`: 指定命名空间下的指定PVC
- `--pvc`: 必须配合namespace使用，或使用完整格式 `namespace/pvc-name`

## 3. 核心功能设计

### 3.1 全局备份状态查看
```bash
# 查看所有DaemonSet Pod的PVC备份状态
local-pvc-backup status [过滤参数]

# 示例
local-pvc-backup status --all
local-pvc-backup status --namespace app
local-pvc-backup status --node worker-1
local-pvc-backup status --namespace app --pvc data
local-pvc-backup status --node worker-1 --namespace app
```

**输出格式：**
```
NODE      | NAMESPACE | PVC           | LAST BACKUP         | SNAPSHOTS | SIZE
worker-1  | app       | data          | 2025-07-21 10:30:00 | 7         | 1.2GB
worker-2  | db        | mysql-data    | 2025-07-21 10:30:00 | 7         | 2.5GB
worker-1  | web       | uploads       | 2025-07-21 10:30:00 | 7         | 500MB
```

### 3.2 单个/批量PVC备份
```bash
# 立即备份
local-pvc-backup backup [过滤参数]

# 示例
local-pvc-backup backup --namespace app --pvc data
local-pvc-backup backup --namespace app              # 备份app命名空间所有PVC
local-pvc-backup backup --node worker-1              # 备份worker-1节点所有PVC
local-pvc-backup backup --all                        # 备份所有启用的PVC
```

### 3.3 快照查看和管理
```bash
# 查看快照
local-pvc-backup snapshots [过滤参数] [输出参数]

# 输出参数
--format table|json|yaml       # 输出格式
--limit <number>               # 限制显示数量
--sort-by time|size           # 排序方式

# 示例
local-pvc-backup snapshots --namespace app --pvc data
local-pvc-backup snapshots --namespace app --format table
local-pvc-backup snapshots --node worker-1 --limit 10
```

**输出示例：**
```
NAMESPACE | PVC      | SNAPSHOT ID  | DATE                | SIZE   | NODE
app       | data     | abc123def456 | 2025-07-21 10:30:00 | 1.2GB  | worker-1
app       | data     | def456ghi789 | 2025-07-20 10:30:00 | 1.1GB  | worker-1
db        | mysql    | ghi789jkl012 | 2025-07-21 10:30:00 | 2.5GB  | worker-2
```

### 3.4 智能时间点恢复
```bash
# 时间点恢复
local-pvc-backup restore [过滤参数] [恢复参数]

# 恢复参数
--time "YYYY-MM-DD HH:MM:SS"   # 指定时间点（智能匹配最近快照）
--snapshot <snapshot-id>        # 指定快照ID
--target-path <path>           # 恢复目标路径（可选，默认原位置）
--dry-run                      # 仅显示恢复计划，不执行

# 示例
local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace app
local-pvc-backup restore --time "2025-07-20 15:30:00" --all --dry-run
local-pvc-backup restore --snapshot abc123def456 --namespace app --pvc data
local-pvc-backup restore --node worker-1 --time "2025-07-20 15:30:00"
```

## 4. 技术实现方案

### 4.1 命令架构
```go
// 新增命令结构
var (
    statusCmd = &cobra.Command{
        Use:   "status",
        Short: "Show PVC backup status",
        Run:   runStatusCommand,
    }
    
    backupCmd = &cobra.Command{
        Use:   "backup", 
        Short: "Trigger immediate backup",
        Run:   runBackupCommand,
    }
    
    snapshotsCmd = &cobra.Command{
        Use:   "snapshots",
        Short: "List snapshots", 
        Run:   runSnapshotsCommand,
    }
    
    restoreCmd = &cobra.Command{
        Use:   "restore",
        Short: "Restore from backup",
        Run:   runRestoreCommand,
    }
)

// 通用过滤参数结构
type FilterOptions struct {
    Node      string
    Namespace string
    PVC       string
    All       bool
}
```

### 4.2 跨节点通信方案

**选择：K8s API + Exec模式（推荐）**
- CLI通过K8s API找到目标节点的Pod
- 使用kubectl exec调用Pod内的命令
- 优点：无需额外网络配置，安全性好
- 缺点：需要kubectl权限

### 4.3 数据聚合设计

```go
type PVCDiscovery struct {
    k8sClient kubernetes.Interface
}

func (d *PVCDiscovery) GetPVCsByFilter(filter FilterOptions) ([]GlobalPVCInfo, error) {
    var pvcs []GlobalPVCInfo
    
    // 1. 根据过滤条件获取相关Pod
    pods := d.getFilteredPods(filter)
    
    // 2. 从Pod中提取PVC信息
    for _, pod := range pods {
        pvcInfos := d.extractPVCInfo(pod)
        pvcs = append(pvcs, pvcInfos...)
    }
    
    // 3. 去重并返回
    return d.deduplicatePVCs(pvcs), nil
}

type GlobalPVCInfo struct {
    PVCName       string
    Namespace     string  
    NodeName      string
    PodName       string
    LastBackup    time.Time
    SnapshotCount int
    TotalSize     string
    BackupEnabled bool
}
```

### 4.4 快照管理增强

```go
// 扩展restic client
func (c *Client) ListSnapshots(ctx context.Context, pvcID string) ([]SnapshotInfo, error) {
    // 执行 restic snapshots --json --tag pvc-id={pvcID}
}

func (c *Client) FindSnapshotByTime(ctx context.Context, pvcID string, targetTime time.Time) (*SnapshotInfo, error) {
    snapshots, err := c.ListSnapshots(ctx, pvcID)
    if err != nil {
        return nil, err
    }
    
    // 找到目标时间之前最近的快照
    var closest *SnapshotInfo
    for _, snapshot := range snapshots {
        if snapshot.Time.Before(targetTime) || snapshot.Time.Equal(targetTime) {
            if closest == nil || snapshot.Time.After(closest.Time) {
                closest = &snapshot
            }
        }
    }
    
    return closest, nil
}

func (c *Client) RestoreSnapshot(ctx context.Context, snapshotID string, targetPath string) error {
    // 执行 restic restore {snapshotID} --target {targetPath}
}

type SnapshotInfo struct {
    ID       string
    Time     time.Time
    Size     int64
    Tags     map[string]string
    Hostname string
}
```

### 4.5 时间点恢复逻辑

```go
type RestorePlan struct {
    PVCs []PVCRestoreInfo
}

type PVCRestoreInfo struct {
    PVCName      string
    Namespace    string
    NodeName     string
    SnapshotID   string
    SnapshotTime time.Time
    TargetPath   string
}

func (m *Manager) CreateRestorePlan(targetTime time.Time, filter FilterOptions) (*RestorePlan, error) {
    // 1. 根据过滤条件获取PVC列表
    pvcs, err := m.discovery.GetPVCsByFilter(filter)
    if err != nil {
        return nil, err
    }
    
    plan := &RestorePlan{}
    
    // 2. 为每个PVC查找最适合的快照
    for _, pvc := range pvcs {
        snapshot, err := m.resticClient.FindSnapshotByTime(ctx, pvc.UID, targetTime)
        if err != nil {
            return nil, fmt.Errorf("failed to find snapshot for PVC %s/%s: %v", pvc.Namespace, pvc.PVCName, err)
        }
        
        if snapshot != nil {
            plan.PVCs = append(plan.PVCs, PVCRestoreInfo{
                PVCName:      pvc.PVCName,
                Namespace:    pvc.Namespace,
                NodeName:     pvc.NodeName,
                SnapshotID:   snapshot.ID,
                SnapshotTime: snapshot.Time,
                TargetPath:   pvc.Path,
            })
        }
    }
    
    return plan, nil
}
```

## 5. 用户交互流程示例

### 5.1 查看备份状态
```bash
$ local-pvc-backup status --all
NODE      | NAMESPACE | PVC           | LAST BACKUP         | SNAPSHOTS | SIZE
worker-1  | app       | data          | 2025-07-21 10:30:00 | 7         | 1.2GB
worker-2  | db        | mysql-data    | 2025-07-21 10:30:00 | 7         | 2.5GB
worker-1  | web       | uploads       | 2025-07-21 10:30:00 | 7         | 500MB

$ local-pvc-backup status --namespace app
NODE      | NAMESPACE | PVC    | LAST BACKUP         | SNAPSHOTS | SIZE
worker-1  | app       | data   | 2025-07-21 10:30:00 | 7         | 1.2GB
worker-1  | app       | config | 2025-07-21 10:30:00 | 5         | 100MB
```

### 5.2 执行时间点恢复
```bash
$ local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace app --dry-run
Finding snapshots for time: 2025-07-20 15:30:00

Restore Plan:
┌─────────────┬─────────────┬──────────────────┬─────────────────────┬───────────┐
│ NAMESPACE   │ PVC         │ SNAPSHOT ID      │ SNAPSHOT TIME       │ NODE      │
├─────────────┼─────────────┼──────────────────┼─────────────────────┼───────────┤
│ app         │ data        │ abc123def456     │ 2025-07-20 15:25:00 │ worker-1  │
│ app         │ config      │ def456ghi789     │ 2025-07-20 15:28:00 │ worker-1  │
└─────────────┴─────────────┴──────────────────┴─────────────────────┴───────────┘

Note: This is a dry-run. Use without --dry-run to execute.

$ local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace app
Executing restore plan...
✓ Restoring app/data from snapshot abc123def456 on worker-1
✓ Restoring app/config from snapshot def456ghi789 on worker-1
All restores completed successfully.
```

### 5.3 查看快照
```bash
$ local-pvc-backup snapshots --namespace app --pvc data --limit 5
SNAPSHOT ID      | DATE                | SIZE    | TAGS
abc123def456     | 2025-07-21 10:30:00 | 1.2GB  | node=worker-1,pvc-name=data,namespace=app
def456ghi789     | 2025-07-20 10:30:00 | 1.1GB  | node=worker-1,pvc-name=data,namespace=app
ghi789jkl012     | 2025-07-19 10:30:00 | 1.0GB  | node=worker-1,pvc-name=data,namespace=app
```

## 6. 配置和部署

### 6.1 RBAC权限扩展
需要添加对以下资源的权限：
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: local-pvc-backup
rules:
# 现有权限...
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get", "list"]
```

### 6.2 新增环境变量
```yaml
# 支持跨节点发现的配置
- name: DISCOVERY_MODE
  value: "k8s-api"
- name: DAEMONSET_NAME  
  value: "local-pvc-backup"
- name: DAEMONSET_NAMESPACE
  value: "kube-system"
```

## 7. 实现状态

### ✅ 已完成功能 (2025-07-21)

#### 核心架构
- [x] 统一过滤参数解析系统
- [x] PVC发现和聚合逻辑
- [x] 跨节点通信机制 (K8s exec模式)
- [x] 基于PVC label的高效过滤
- [x] 延迟初始化避免help命令需要环境变量

#### 命令实现
- [x] `status` 命令 - 跨节点PVC备份状态聚合
- [x] `snapshots` 命令 - 跨节点快照查询和聚合
- [x] `backup` 命令 - 跨节点并行备份执行
- [x] `restore` 命令 - 智能时间点恢复计划
- [x] `node-exec` 内部命令 - 跨节点通信处理

#### 技术特性
- [x] Restic client扩展 (snapshots查询、时间匹配、恢复)
- [x] 智能时间点快照匹配算法
- [x] 干运行模式 (dry-run)
- [x] 并发执行和错误处理
- [x] 表格格式输出

### 🎯 可用命令示例

```bash
# 查看所有节点的PVC备份状态
./local-pvc-backup status --all

# 查看指定命名空间的快照
./local-pvc-backup snapshots --namespace app --limit 10

# 执行特定PVC的备份
./local-pvc-backup backup --namespace app --pvc data --dry-run

# 时间点恢复
./local-pvc-backup restore --time "2025-07-20 15:30:00" --namespace app --dry-run
```

### 📋 下一步开发 (优化项)

#### 短期优化
- [ ] JSON/YAML输出格式支持
- [ ] 快照恢复功能完善
- [ ] 进度条和实时状态显示
- [ ] 更详细的错误处理和日志

#### 中期增强
- [ ] 配置文件支持
- [ ] Bash/Zsh命令补全
- [ ] 集成测试套件
- [ ] 性能优化和并发控制

#### 长期规划
- [ ] GUI界面
- [ ] 备份策略模板
- [ ] 多集群管理
- [ ] 指标和监控集成
- [ ] 自动故障恢复

### 🛠️ 技术债务

1. **REST API模式** - 当前使用exec模式，未来可考虑HTTP API以提升性能
2. **缓存机制** - 节点状态和快照信息缓存
3. **重试机制** - 网络异常和临时故障的重试逻辑
4. **资源限制** - 并发执行的资源控制和限流