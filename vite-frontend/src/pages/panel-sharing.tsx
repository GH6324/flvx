import { useState, useEffect, useCallback } from "react";
import { Button } from "@heroui/button";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Tabs, Tab } from "@heroui/tabs";
import { Input } from "@heroui/input";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@heroui/modal";
import { Select, SelectItem } from "@heroui/select";
import { toast } from "react-hot-toast";
import {
  getNodeList,
  createPeerShare,
  getPeerShareList,
  deletePeerShare,
  importRemoteNode,
} from "@/api";

interface Node {
  id: number;
  name: string;
  isRemote?: number;
}

interface PeerShare {
  id: number;
  name: string;
  token: string;
  maxBandwidth: number;
  expiryTime: number;
  portRangeStart: number;
  portRangeEnd: number;
  isActive: number;
  allowedDomains?: string;
  allowedIps?: string;
}

export default function PanelSharingPage() {
  const [selectedTab, setSelectedTab] = useState("my-shares");
  const [shares, setShares] = useState<PeerShare[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(false);

  // Modals
  const [createShareOpen, setCreateShareOpen] = useState(false);
  const [importNodeOpen, setImportNodeOpen] = useState(false);

  // Forms
  const [shareForm, setShareForm] = useState({
    name: "",
    nodeId: "",
    maxBandwidth: 0,
    expiryDays: 30,
    portRangeStart: 10000,
    portRangeEnd: 20000,
    allowedDomains: "",
    allowedIps: "",
  });

  const [importForm, setImportForm] = useState({
    remoteUrl: "",
    token: "",
  });

  const loadShares = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getPeerShareList();
      if (res.code === 0) {
        setShares(res.data || []);
      } else {
        toast.error(res.msg || "加载分享列表失败");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const loadNodes = useCallback(async () => {
    try {
      const res = await getNodeList();
      if (res.code === 0) {
        const localNodes: Node[] = (res.data || []).filter(
          (node: Node) => (node?.isRemote ?? 0) !== 1,
        );
        setNodes(localNodes);
        setShareForm((prev) => {
          if (!prev.nodeId) {
            return prev;
          }
          const hasSelectedNode = localNodes.some(
            (node: Node) => String(node.id) === prev.nodeId,
          );
          return hasSelectedNode ? prev : { ...prev, nodeId: "" };
        });
      }
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    if (selectedTab === "my-shares") {
      loadShares();
      loadNodes();
    }
  }, [selectedTab, loadShares, loadNodes]);

  const handleCreateShare = async () => {
    if (!shareForm.name || !shareForm.nodeId) {
      toast.error("请填写必要信息");
      return;
    }
    const nodeId = parseInt(shareForm.nodeId, 10);
    if (Number.isNaN(nodeId) || !nodes.some((node) => node.id === nodeId)) {
      toast.error("仅可选择本地节点");
      return;
    }
    try {
      const expiryTime =
        Date.now() + shareForm.expiryDays * 24 * 60 * 60 * 1000;
      const res = await createPeerShare({
        name: shareForm.name,
        nodeId,
        maxBandwidth: shareForm.maxBandwidth * 1024 * 1024 * 1024,
        expiryTime: shareForm.expiryDays === 0 ? 0 : expiryTime,
        portRangeStart: shareForm.portRangeStart,
        portRangeEnd: shareForm.portRangeEnd,
        allowedDomains: shareForm.allowedDomains,
        allowedIps: shareForm.allowedIps,
      });
      if (res.code === 0) {
        toast.success("创建成功");
        setCreateShareOpen(false);
        loadShares();
      } else {
        toast.error(res.msg || "创建失败");
      }
    } catch {
      toast.error("网络错误");
    }
  };

  const handleDeleteShare = async (id: number) => {
    try {
      const res = await deletePeerShare(id);
      if (res.code === 0) {
        toast.success("删除成功");
        loadShares();
      } else {
        toast.error(res.msg || "删除失败");
      }
    } catch {
      toast.error("网络错误");
    }
  };

  const handleImportNode = async () => {
    if (!importForm.remoteUrl || !importForm.token) {
      toast.error("请填写完整信息");
      return;
    }
    try {
      // Automatically add http/https if missing
      let url = importForm.remoteUrl.trim();
      if (!url.startsWith("http")) {
        url = "http://" + url;
      }
      
      const res = await importRemoteNode({
        remoteUrl: url,
        token: importForm.token.trim(),
      });
      if (res.code === 0) {
        toast.success("导入成功，请前往节点列表查看");
        setImportNodeOpen(false);
        setImportForm({ remoteUrl: "", token: "" });
      } else {
        toast.error(res.msg || "导入失败");
      }
    } catch {
      toast.error("网络错误");
    }
  };

  const copyToken = (token: string) => {
    navigator.clipboard.writeText(token);
    toast.success("Token已复制");
  };

  return (
    <div className="p-4 md:p-6 space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">面板共享 (Panel Peering)</h1>
      </div>

      <Tabs
        aria-label="Options"
        selectedKey={selectedTab}
        onSelectionChange={(k) => setSelectedTab(k as string)}
      >
        <Tab key="my-shares" title="我分享的 (Provider)">
          <Card>
            <CardBody>
              <div className="mb-4">
                <Button color="primary" onPress={() => setCreateShareOpen(true)}>
                  创建分享
                </Button>
              </div>
              
              {loading ? (
                <div className="text-center py-10 text-gray-500">加载中...</div>
              ) : shares.length === 0 ? (
                <div className="text-center py-10 text-gray-500">暂无分享</div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                  {shares.map((share) => (
                    <Card key={share.id} className="border border-divider shadow-sm">
                      <CardHeader className="flex justify-between">
                        <h3 className="font-bold">{share.name}</h3>
                        <Button size="sm" color="danger" variant="flat" onPress={() => handleDeleteShare(share.id)}>删除</Button>
                      </CardHeader>
                      <CardBody className="text-sm space-y-2">
                        <p>端口范围: {share.portRangeStart} - {share.portRangeEnd}</p>
                        {share.allowedDomains && <p>允许域名: {share.allowedDomains}</p>}
                        {share.allowedIps && <p>允许API IP: {share.allowedIps}</p>}
                        <p>过期时间: {share.expiryTime === 0 ? "永久" : new Date(share.expiryTime).toLocaleDateString()}</p>
                        <div className="flex gap-2">
                          <Input readOnly size="sm" value={share.token} />
                          <Button size="sm" onPress={() => copyToken(share.token)}>复制</Button>
                        </div>
                      </CardBody>
                    </Card>
                  ))}
                </div>
              )}
            </CardBody>
          </Card>
        </Tab>
        <Tab key="remote-nodes" title="远程节点 (Consumer)">
          <Card>
            <CardBody>
              <div className="mb-4">
                <Button color="secondary" onPress={() => setImportNodeOpen(true)}>
                  导入远程节点
                </Button>
              </div>
              <div className="text-center py-10 text-gray-500">
                <p>已导入的节点将显示在“节点管理”页面，带有“远程”标记。</p>
                <p className="mt-2">请使用其创建隧道。</p>
              </div>
            </CardBody>
          </Card>
        </Tab>
      </Tabs>

      {/* Create Share Modal */}
      <Modal isOpen={createShareOpen} onClose={() => setCreateShareOpen(false)}>
        <ModalContent>
          <ModalHeader>创建分享</ModalHeader>
          <ModalBody>
            <Input
              label="名称"
              placeholder="备注名称"
              value={shareForm.name}
              onChange={(e) => setShareForm({ ...shareForm, name: e.target.value })}
            />
            <Select
              label="选择节点"
              placeholder="选择要分享的本地节点"
              selectedKeys={shareForm.nodeId ? [shareForm.nodeId] : []}
              onChange={(e) => setShareForm({ ...shareForm, nodeId: e.target.value })}
            >
              {nodes.map((node) => (
                <SelectItem key={node.id} textValue={node.name}>
                  {node.name}
                </SelectItem>
              ))}
            </Select>
            <div className="flex gap-4">
              <Input
                label="起始端口"
                type="number"
                value={shareForm.portRangeStart.toString()}
                onChange={(e) => setShareForm({ ...shareForm, portRangeStart: parseInt(e.target.value) })}
              />
              <Input
                label="结束端口"
                type="number"
                value={shareForm.portRangeEnd.toString()}
                onChange={(e) => setShareForm({ ...shareForm, portRangeEnd: parseInt(e.target.value) })}
              />
            </div>
            <Input
              label="有效期 (天)"
              type="number"
              description="0 表示永久"
              value={shareForm.expiryDays.toString()}
              onChange={(e) => setShareForm({ ...shareForm, expiryDays: parseInt(e.target.value) })}
            />
            <Input
              label="允许的域名 (可选)"
              placeholder="example.com, panel.test.com"
              description="限制使用此Token的来源面板域名，多个域名用逗号分隔，留空不限制"
              value={shareForm.allowedDomains}
              onChange={(e) => setShareForm({ ...shareForm, allowedDomains: e.target.value })}
            />
            <Input
              label="允许的API IP (可选)"
              placeholder="203.0.113.10, 2001:db8::10, 198.51.100.0/24"
              description="仅白名单IP可导入此分享，支持IPv4/IPv6/CIDR，多个用逗号分隔"
              value={shareForm.allowedIps}
              onChange={(e) => setShareForm({ ...shareForm, allowedIps: e.target.value })}
            />
          </ModalBody>
          <ModalFooter>
            <Button onPress={() => setCreateShareOpen(false)}>取消</Button>
            <Button color="primary" onPress={handleCreateShare}>创建</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Import Node Modal */}
      <Modal isOpen={importNodeOpen} onClose={() => setImportNodeOpen(false)}>
        <ModalContent>
          <ModalHeader>导入远程节点</ModalHeader>
          <ModalBody>
            <Input
              label="远程面板地址"
              placeholder="http://panel.example.com:8088"
              value={importForm.remoteUrl}
              onChange={(e) => setImportForm({ ...importForm, remoteUrl: e.target.value })}
            />
            <Input
              label="Token"
              placeholder="Bearer Token"
              value={importForm.token}
              onChange={(e) => setImportForm({ ...importForm, token: e.target.value })}
            />
          </ModalBody>
          <ModalFooter>
            <Button onPress={() => setImportNodeOpen(false)}>取消</Button>
            <Button color="secondary" onPress={handleImportNode}>导入</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
