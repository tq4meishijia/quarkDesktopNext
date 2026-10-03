export namespace app {
	
	export class Profile {
	    nickname: string;
	    avatar: string;
	    memberType: string;
	    usedBytes: number;
	    totalBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nickname = source["nickname"];
	        this.avatar = source["avatar"];
	        this.memberType = source["memberType"];
	        this.usedBytes = source["usedBytes"];
	        this.totalBytes = source["totalBytes"];
	    }
	}
	export class AuthState {
	    loggedIn: boolean;
	    source: string;
	    masked: string;
	    message: string;
	    profile: Profile;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loggedIn = source["loggedIn"];
	        this.source = source["source"];
	        this.masked = source["masked"];
	        this.message = source["message"];
	        this.profile = this.convertValues(source["profile"], Profile);
	        this.reason = source["reason"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CredentialInfo {
	    hasCredential: boolean;
	    source: string;
	    itemCount: number;
	    sessionFile: string;
	    loggedIn: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CredentialInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasCredential = source["hasCredential"];
	        this.source = source["source"];
	        this.itemCount = source["itemCount"];
	        this.sessionFile = source["sessionFile"];
	        this.loggedIn = source["loggedIn"];
	    }
	}
	export class FileItem {
	    fid: string;
	    name: string;
	    path: string;
	    size: number;
	    isDir: boolean;
	    mtime: number;
	    ctime: number;
	    ext: string;
	
	    static createFrom(source: any = {}) {
	        return new FileItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fid = source["fid"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.mtime = source["mtime"];
	        this.ctime = source["ctime"];
	        this.ext = source["ext"];
	    }
	}
	export class DirListing {
	    path: string;
	    items: FileItem[];
	    reachedCap: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirListing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.items = this.convertValues(source["items"], FileItem);
	        this.reachedCap = source["reachedCap"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DownloadItem {
	    fid: string;
	    name: string;
	    size: number;
	    remotePath: string;
	
	    static createFrom(source: any = {}) {
	        return new DownloadItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fid = source["fid"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.remotePath = source["remotePath"];
	    }
	}
	export class DownloaderInfo {
	    id: string;
	    label: string;
	    note: string;
	    kind: string;
	    path: string;
	    ready: boolean;
	    defaultArgs: string[];
	
	    static createFrom(source: any = {}) {
	        return new DownloaderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.note = source["note"];
	        this.kind = source["kind"];
	        this.path = source["path"];
	        this.ready = source["ready"];
	        this.defaultArgs = source["defaultArgs"];
	    }
	}
	
	export class InteractiveLoginState {
	    active: boolean;
	    phase: string;
	    hint: string;
	    url: string;
	    collected: number;
	    remaining: number;
	    staleExisting: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InteractiveLoginState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.phase = source["phase"];
	        this.hint = source["hint"];
	        this.url = source["url"];
	        this.collected = source["collected"];
	        this.remaining = source["remaining"];
	        this.staleExisting = source["staleExisting"];
	    }
	}
	export class MyShareItem {
	    shareId: string;
	    title: string;
	    url: string;
	    passcode: string;
	    viewCnt: number;
	    saveCnt: number;
	    createdAt: number;
	    expired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MyShareItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shareId = source["shareId"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.passcode = source["passcode"];
	        this.viewCnt = source["viewCnt"];
	        this.saveCnt = source["saveCnt"];
	        this.createdAt = source["createdAt"];
	        this.expired = source["expired"];
	    }
	}
	
	export class SaveShareRequest {
	    pwdId: string;
	    stoken: string;
	    fids: string[];
	    tokens: string[];
	    targetFid: string;
	    saveAll: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveShareRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pwdId = source["pwdId"];
	        this.stoken = source["stoken"];
	        this.fids = source["fids"];
	        this.tokens = source["tokens"];
	        this.targetFid = source["targetFid"];
	        this.saveAll = source["saveAll"];
	    }
	}
	export class SaveShareResult {
	    ok: boolean;
	    message: string;
	    taskId: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveShareResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.taskId = source["taskId"];
	    }
	}
	export class ShareLink {
	    url: string;
	    passcode: string;
	    pwdId: string;
	    expiresAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ShareLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.passcode = source["passcode"];
	        this.pwdId = source["pwdId"];
	        this.expiresAt = source["expiresAt"];
	    }
	}
	export class ShareNode {
	    fid: string;
	    name: string;
	    size: number;
	    isDir: boolean;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new ShareNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fid = source["fid"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.token = source["token"];
	    }
	}
	export class SharePreview {
	    pwdId: string;
	    passcode: string;
	    stoken: string;
	    title: string;
	    items: ShareNode[];
	
	    static createFrom(source: any = {}) {
	        return new SharePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pwdId = source["pwdId"];
	        this.passcode = source["passcode"];
	        this.stoken = source["stoken"];
	        this.title = source["title"];
	        this.items = this.convertValues(source["items"], ShareNode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TaskDTO {
	    id: string;
	    kind: string;
	    name: string;
	    localPath: string;
	    dest: string;
	    remotePath: string;
	    size: number;
	    done: number;
	    progress: number;
	    status: string;
	    speed: number;
	    error: string;
	    engine: string;
	    createdAt: number;
	    finishedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TaskDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.localPath = source["localPath"];
	        this.dest = source["dest"];
	        this.remotePath = source["remotePath"];
	        this.size = source["size"];
	        this.done = source["done"];
	        this.progress = source["progress"];
	        this.status = source["status"];
	        this.speed = source["speed"];
	        this.error = source["error"];
	        this.engine = source["engine"];
	        this.createdAt = source["createdAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}

}

export namespace config {
	
	export class Settings {
	    downloadDir: string;
	    concurrency: number;
	    theme: string;
	    uploadPolicy: string;
	    startMinimized: boolean;
	    downloader: string;
	    downloaderExec: string;
	    downloaderArgs: string[];
	    segments: number;
	    sameName: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadDir = source["downloadDir"];
	        this.concurrency = source["concurrency"];
	        this.theme = source["theme"];
	        this.uploadPolicy = source["uploadPolicy"];
	        this.startMinimized = source["startMinimized"];
	        this.downloader = source["downloader"];
	        this.downloaderExec = source["downloaderExec"];
	        this.downloaderArgs = source["downloaderArgs"];
	        this.segments = source["segments"];
	        this.sameName = source["sameName"];
	    }
	}

}

