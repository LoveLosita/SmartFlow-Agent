package commenttree

import (
	"fmt"
	"sort"
	"time"

	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	forumcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclassforum"
)

const deletedCommentPlaceholder = "该评论已删除"

type commentTreeNode struct {
	comment  forummodel.ForumComment
	parent   *commentTreeNode
	children []*commentTreeNode
}

// BuildForumCommentTree 将扁平评论组装为多层评论树。
//
// 职责边界：
// 1. 负责根据 parent_comment_id 组装无限层树结构，并按 CreatedAt 升序稳定排序；
// 2. 负责把软删除评论转换成前端展示文案，同时保留 deleted 状态与 deleted_at；
// 3. 不负责查询数据库、补充真实昵称头像，也不负责帖子级权限校验。
func BuildForumCommentTree(comments []forummodel.ForumComment, actorUserID uint64) []forumcontracts.ForumCommentNode {
	if len(comments) == 0 {
		return nil
	}

	nodesByID := make(map[uint64]*commentTreeNode, len(comments))
	orderedNodes := make([]*commentTreeNode, 0, len(comments))
	for i := range comments {
		node := &commentTreeNode{comment: comments[i]}
		nodesByID[comments[i].ID] = node
		orderedNodes = append(orderedNodes, node)
	}

	roots := attachCommentTreeNodes(orderedNodes, nodesByID)
	sortCommentTreeChildren(roots)

	result := make([]forumcontracts.ForumCommentNode, 0, len(roots))
	for i := range roots {
		result = append(result, buildForumCommentNode(roots[i], actorUserID))
	}
	return result
}

// attachCommentTreeNodes 按原始 parent_comment_id 把评论挂成树。
//
// 职责边界：
// 1. 只处理节点挂载关系，不做字段格式化；
// 2. 缺失父节点、自指向、环引用都回退到根层，避免整棵树丢失；
// 3. 根层顺序先保留输入顺序，后续统一由排序函数做稳定排序。
func attachCommentTreeNodes(
	orderedNodes []*commentTreeNode,
	nodesByID map[uint64]*commentTreeNode,
) []*commentTreeNode {
	roots := make([]*commentTreeNode, 0, len(orderedNodes))
	for i := range orderedNodes {
		node := orderedNodes[i]
		parentID := node.comment.ParentCommentID
		if parentID == nil {
			roots = append(roots, node)
			continue
		}

		parentNode, ok := nodesByID[*parentID]
		if !ok || parentNode == nil || parentNode.comment.ID == node.comment.ID {
			roots = append(roots, node)
			continue
		}

		if wouldCreateCommentCycle(nodesByID, node.comment.ID, parentNode) {
			roots = append(roots, node)
			continue
		}

		node.parent = parentNode
		parentNode.children = append(parentNode.children, node)
	}
	return roots
}

// wouldCreateCommentCycle 判断把 child 挂到 parent 下时是否会形成环。
//
// 职责边界：
// 1. 只依赖原始 parent_comment_id 链路判断，不依赖当前挂载顺序；
// 2. 一旦发现 child 会回到自己，或父链本身已成环，就返回 true；
// 3. 父链中途断开时按“无环”处理，让节点继续挂到可用分支上。
func wouldCreateCommentCycle(
	nodesByID map[uint64]*commentTreeNode,
	childCommentID uint64,
	parentNode *commentTreeNode,
) bool {
	visited := make(map[uint64]struct{})
	current := parentNode
	for current != nil {
		if current.comment.ID == childCommentID {
			return true
		}
		if _, seen := visited[current.comment.ID]; seen {
			return true
		}
		visited[current.comment.ID] = struct{}{}

		if current.comment.ParentCommentID == nil {
			return false
		}

		nextNode, ok := nodesByID[*current.comment.ParentCommentID]
		if !ok {
			return false
		}
		current = nextNode
	}
	return false
}

// sortCommentTreeChildren 对根层以下的兄弟节点做 CreatedAt 升序稳定排序。
//
// 职责边界：
// 1. 根层顺序来自服务层根评论分页，必须保留 latest/oldest 的查询语义；
// 2. 子回复统一按 CreatedAt 升序展示，符合常见对话阅读顺序；
// 3. 相同 CreatedAt 依赖稳定排序保留原始输入顺序，避免同秒回复来回跳动。
func sortCommentTreeChildren(nodes []*commentTreeNode) {
	if len(nodes) == 0 {
		return
	}

	for i := range nodes {
		sort.SliceStable(nodes[i].children, func(left, right int) bool {
			return nodes[i].children[left].comment.CreatedAt.Before(nodes[i].children[right].comment.CreatedAt)
		})
		sortCommentTreeChildren(nodes[i].children)
	}
}

// buildForumCommentNode 把内部树节点转换成对外契约节点。
//
// 职责边界：
// 1. 负责软删除展示文案、CanDelete、时间格式等输出字段整理；
// 2. 根节点统一输出 nil parent_comment_id；孤儿兜底到根层后也遵循该规则；
// 3. 这里沿用当前服务里的“用户{ID}”占位昵称语义。
func buildForumCommentNode(node *commentTreeNode, actorUserID uint64) forumcontracts.ForumCommentNode {
	children := make([]forumcontracts.ForumCommentNode, 0, len(node.children))
	for i := range node.children {
		children = append(children, buildForumCommentNode(node.children[i], actorUserID))
	}

	// 1. 先基于最终挂载结果回填 parent_comment_id，保证孤儿回退到根层后对外语义一致。
	// 2. 再处理软删除评论文案：内容固定替换，但 status 仍保留 deleted，便于前端区分。
	// 3. 最后按“当前用户且评论可见”计算 CanDelete，避免已删除评论被重复展示可删除按钮。
	parentCommentID := actualParentCommentID(node.parent)
	content := node.comment.Content
	if node.comment.Status == forummodel.ForumCommentStatusDeleted {
		content = deletedCommentPlaceholder
	}

	return forumcontracts.ForumCommentNode{
		CommentID:       node.comment.ID,
		PostID:          node.comment.PostID,
		ParentCommentID: parentCommentID,
		Content:         content,
		Status:          node.comment.Status,
		Author:          buildCommentAuthor(node.comment.UserID),
		CanDelete:       node.comment.Status == forummodel.ForumCommentStatusVisible && node.comment.UserID == actorUserID,
		CreatedAt:       formatCommentTime(node.comment.CreatedAt),
		DeletedAt:       formatCommentTimePtr(node.comment.DeletedAt),
		Children:        children,
	}
}

func actualParentCommentID(parent *commentTreeNode) *uint64 {
	if parent == nil {
		return nil
	}
	parentID := parent.comment.ID
	return &parentID
}

func formatCommentTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatCommentTimePtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func buildCommentAuthor(userID uint64) forumcontracts.UserBrief {
	// 由于本轮写入范围被限制在 commenttree/tree.go，暂时无法把 UserBrief 生成逻辑下沉成公共能力；
	// 这里先与现有服务层保持同一占位昵称语义，避免为了抽公共层去改动 sv/contract 等非授权文件。
	return forumcontracts.UserBrief{
		UserID:   userID,
		Nickname: fmt.Sprintf("用户%d", userID),
	}
}
