好的，我来解释分支开发的完整流程。

## 分支的概念

```
master (主分支，保持稳定)
  │├─── feature/nats-replacement (功能分支，开发 NATS 替换)
  │
  └─── feature/user-system (另一个功能分支，开发用户系统)
```

每个功能在独立分支上开发，互不干扰，完成后合并回master。

---

## 完整开发流程

### 第一步：创建并切换到功能分支

```bash
# 从master 创建新分支
git checkout -b feature/nats-replacement

# 这条命令做了两件事：
# 1. 创建分支 feature/nats-replacement
# 2. 切换到这个分支
```

此时你在 `feature/nats-replacement` 分支上，所有改动都在这个分支。

### 第二步：开发 + 提交（循环多次）

```bash
# 修改代码...
# 比如：实现 MQ interface

# 查看改动
git status

# 添加改动
git add internal/logic/dao/mq.go

# 提交到本地
git commit -m "feat: 抽象 MQ interface"

# 继续修改...
# 比如：实现 NATS 适配器

git add internal/logic/dao/nats.go
git commit -m "feat: 实现 NATS Publisher"

# 再继续...
git add internal/job/consumer.go
git commit -m "feat: Job 接入 NATS Consumer"
```

**关键**：每完成一个小功能就commit 一次，不要攒一大堆改动。

### 第三步：推送到远程

```bash
# 第一次推送这个分支
git push origin feature/nats-replacement

# 后续推送（已经建立追踪关系）
git push
```

此时 GitHub 上会出现 `feature/nats-replacement` 分支。

### 第四步：功能完成，合并回 master

```bash
# 1. 切换回 master
git checkout master

# 2. 合并功能分支
git merge feature/nats-replacement

# 3. 推送 master
git push origin master

# 4. 删除功能分支（可选）
git branch -d feature/nats-replacement           # 删除本地分支
git push origin --delete feature/nats-replacement # 删除远程分支
```

---

## 实际场景示例

### 场景 1：NATS 替换（单人开发）

```bash
# Day 1
git checkout -b feature/nats-replacement
# 写代码...
git add .
git commit -m "feat:抽象 MQ interface"
git push origin feature/nats-replacement

# Day 2
# 继续在同一分支写代码...
git add .
git commit -m "feat: 实现 NATS 适配器"
git push

# Day 3
# 完成了，合并回 master
git checkout master
git merge feature/nats-replacement
git push origin master
```

### 场景 2：同时开发多个功能

```bash
# 开发 NATS 替换
git checkout -b feature/nats-replacement
# 写代码...
git commit -m "feat: NATS 部分完成"
git push origin feature/nats-replacement

# 切换到另一个功能（NATS 还没完成，先放着）
git checkout master
git checkout -b feature/user-system
# 写用户系统...
git commit -m "feat: 用户注册接口"
git push origin feature/user-system

# 用户系统完成了，先合并
git checkout master
git merge feature/user-system
git push origin master

# 再回去继续 NATS
git checkout feature/nats-replacement
# 继续写...
```

---

## 常用命令速查

```bash
# 查看所有分支
git branch -a

# 查看当前在哪个分支
git branch

# 切换分支
git checkout <分支名>

# 创建并切换
git checkout -b <新分支名>

# 删除本地分支
git branch -d <分支名>

# 查看分支历史
git log --oneline --graph --all
```

---

## 推荐的分支命名

```
feature/nats-replacement# 新功能
feature/user-system
feature/group-chat

fix/heartbeat-timeout# Bug 修复

docs/benchmark-guide# 文档

refactor/remove-tcp# 重构
```

前缀让分支用途一目了然。