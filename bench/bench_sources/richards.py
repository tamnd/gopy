# richards — classic Stanford Richards benchmark (Python port). BSD.
# Trimmed to one full run (no warmup loop).

import sys, os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

# Task IDs
I_IDLE, I_WORK, I_HANDLERA, I_HANDLERB, I_DEVA, I_DEVB = 1, 2, 3, 4, 5, 6

class Packet(object):
    def __init__(self, l, i, k):
        self.link = l
        self.ident = i
        self.kind = k
        self.datum = 0
        self.data = [0]*4
    def append_to(self, lst):
        self.link = None
        if lst is None:
            return self
        p = lst
        while p.link is not None:
            p = p.link
        p.link = self
        return lst

class TaskState(object):
    def __init__(self):
        self.packet_pending = True
        self.task_waiting = False
        self.task_holding = False
    def packetPending(self):
        self.packet_pending = True; self.task_waiting = False; self.task_holding = False; return self
    def waiting(self):
        self.packet_pending = False; self.task_waiting = True; self.task_holding = False; return self
    def running(self):
        self.packet_pending = False; self.task_waiting = False; self.task_holding = False; return self
    def waitingWithPacket(self):
        self.packet_pending = True; self.task_waiting = True; self.task_holding = False; return self
    def isPacketPending(self): return self.packet_pending
    def isTaskWaiting(self):   return self.task_waiting
    def isTaskHolding(self):   return self.task_holding
    def isTaskHoldingOrWaiting(self): return self.task_holding or (not self.packet_pending and self.task_waiting)
    def isWaitingWithPacket(self):    return self.packet_pending and self.task_waiting and not self.task_holding

tracing = False
layout = 0

class TaskWorkArea(object):
    def __init__(self):
        self.taskTab = [None]*10
        self.taskList = None
        self.holdCount = 0
        self.qpktCount = 0

taskWorkArea = TaskWorkArea()

class Task(TaskState):
    def __init__(self, i, p, w, initialState, r):
        self.link = taskWorkArea.taskList
        self.ident = i; self.priority = p; self.input = w
        self.packet_pending = initialState.isPacketPending()
        self.task_waiting   = initialState.isTaskWaiting()
        self.task_holding   = initialState.isTaskHolding()
        self.handle = r
        taskWorkArea.taskList = self
        taskWorkArea.taskTab[i] = self
    def fn(self, pkt, r):
        raise NotImplementedError
    def addPacket(self, p, old):
        if self.input is None:
            self.input = p
            self.packet_pending = True
            if self.priority > old.priority:
                return self
        else:
            p.append_to(self.input)
        return old
    def runTask(self):
        if self.isWaitingWithPacket():
            msg = self.input
            self.input = msg.link
            self.packet_pending = (self.input is not None)
            return self.fn(msg, self.handle)
        return self.fn(None, self.handle)
    def waitTask(self):
        self.task_waiting = True
        return self
    def hold(self):
        taskWorkArea.holdCount += 1
        self.task_holding = True
        return self.link
    def release(self, i):
        t = self.findtcb(i)
        t.task_holding = False
        if t.priority > self.priority:
            return t
        return self
    def qpkt(self, pkt):
        t = self.findtcb(pkt.ident)
        taskWorkArea.qpktCount += 1
        pkt.link = None
        pkt.ident = self.ident
        return t.addPacket(pkt, self)
    def findtcb(self, i):
        t = taskWorkArea.taskTab[i]
        return t

class DeviceTaskRec(object):
    def __init__(self): self.pending = None

class IdleTaskRec(object):
    def __init__(self): self.control = 1; self.count = 10000

class HandlerTaskRec(object):
    def __init__(self): self.work_in = None; self.device_in = None
    def workInAdd(self, p): self.work_in = p.append_to(self.work_in); return self.work_in
    def deviceInAdd(self, p): self.device_in = p.append_to(self.device_in); return self.device_in

class WorkerTaskRec(object):
    def __init__(self): self.destination = I_HANDLERA; self.count = 0

class DeviceTask(Task):
    def __init__(self, i, p, w, s, r): Task.__init__(self, i, p, w, s, r)
    def fn(self, pkt, r):
        d = r
        if pkt is None:
            pkt = d.pending
            if pkt is None: return self.waitTask()
            d.pending = None
            return self.qpkt(pkt)
        d.pending = pkt
        if tracing: pass
        return self.hold()

class HandlerTask(Task):
    def __init__(self, i, p, w, s, r): Task.__init__(self, i, p, w, s, r)
    def fn(self, pkt, r):
        h = r
        if pkt is not None:
            if pkt.kind == 1: h.workInAdd(pkt)
            else: h.deviceInAdd(pkt)
        work = h.work_in
        if work is None: return self.waitTask()
        count = work.datum
        if count >= 4:
            h.work_in = work.link
            return self.qpkt(work)
        dev = h.device_in
        if dev is None: return self.waitTask()
        h.device_in = dev.link
        dev.datum = work.data[count]
        work.datum = count + 1
        return self.qpkt(dev)

class IdleTask(Task):
    def __init__(self, i, p, w, s, r): Task.__init__(self, i, 0, None, s, r)
    def fn(self, pkt, r):
        i = r
        i.count -= 1
        if i.count == 0: return self.hold()
        if (i.control & 1) == 0:
            i.control //= 2; return self.release(I_DEVA)
        i.control = (i.control // 2) ^ 0xd008
        return self.release(I_DEVB)

class WorkTask(Task):
    def __init__(self, i, p, w, s, r): Task.__init__(self, i, p, w, s, r)
    def fn(self, pkt, r):
        w = r
        if pkt is None: return self.waitTask()
        if w.destination == I_HANDLERA: dest = I_HANDLERB
        else: dest = I_HANDLERA
        w.destination = dest
        pkt.ident = dest
        pkt.datum = 0
        for i in range(4):
            w.count += 1
            if w.count > 26: w.count = 1
            pkt.data[i] = 65 + w.count - 1
        return self.qpkt(pkt)

def schedule():
    t = taskWorkArea.taskList
    while t is not None:
        if t.isTaskHoldingOrWaiting():
            t = t.link
        else:
            t = t.runTask()

def main():
    taskWorkArea.holdCount = 0
    taskWorkArea.qpktCount = 0
    IdleTask(I_IDLE, 0, None, TaskState().running(), IdleTaskRec())
    wkq = Packet(None, 0, 0)
    wkq = Packet(wkq, 0, 0)
    WorkTask(I_WORK, 1000, wkq, TaskState().waitingWithPacket(), WorkerTaskRec())
    wkq = Packet(None, I_DEVA, 1)
    wkq = Packet(wkq, I_DEVA, 1)
    wkq = Packet(wkq, I_DEVA, 1)
    HandlerTask(I_HANDLERA, 2000, wkq, TaskState().waitingWithPacket(), HandlerTaskRec())
    wkq = Packet(None, I_DEVB, 1)
    wkq = Packet(wkq, I_DEVB, 1)
    wkq = Packet(wkq, I_DEVB, 1)
    HandlerTask(I_HANDLERB, 3000, wkq, TaskState().waitingWithPacket(), HandlerTaskRec())
    DeviceTask(I_DEVA, 4000, None, TaskState().waiting(), DeviceTaskRec())
    DeviceTask(I_DEVB, 5000, None, TaskState().waiting(), DeviceTaskRec())
    schedule()

for _ in range(max(1, 3 // _S)):
    main()
