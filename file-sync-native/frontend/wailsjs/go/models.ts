export namespace main {
	
	export class Settings {
	    backup_root: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backup_root = source["backup_root"];
	    }
	}
	export class TaskInput {
	    name: string;
	    source_path: string;
	    target_path: string;
	    ignore_rules: string[];
	
	    static createFrom(source: any = {}) {
	        return new TaskInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.source_path = source["source_path"];
	        this.target_path = source["target_path"];
	        this.ignore_rules = source["ignore_rules"];
	    }
	}

}

export namespace models {
	
	export class FileDiff {
	    added: string[];
	    modified: string[];
	    deleted: string[];
	
	    static createFrom(source: any = {}) {
	        return new FileDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.modified = source["modified"];
	        this.deleted = source["deleted"];
	    }
	}
	export class SyncProgress {
	    task_id: string;
	    status: string;
	    scanned_files: number;
	    current_path: string;
	    total_files: number;
	    done_files: number;
	    total_bytes: number;
	    done_bytes: number;
	    speed_bps: number;
	    eta_seconds: number;
	    percentage: number;
	    pending_deletes?: string[];
	    error_message?: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.scanned_files = source["scanned_files"];
	        this.current_path = source["current_path"];
	        this.total_files = source["total_files"];
	        this.done_files = source["done_files"];
	        this.total_bytes = source["total_bytes"];
	        this.done_bytes = source["done_bytes"];
	        this.speed_bps = source["speed_bps"];
	        this.eta_seconds = source["eta_seconds"];
	        this.percentage = source["percentage"];
	        this.pending_deletes = source["pending_deletes"];
	        this.error_message = source["error_message"];
	    }
	}
	export class SyncTask {
	    id: string;
	    name: string;
	    source_path: string;
	    target_path: string;
	    ignore_rules: string[];
	    // Go type: time
	    last_sync: any;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source_path = source["source_path"];
	        this.target_path = source["target_path"];
	        this.ignore_rules = source["ignore_rules"];
	        this.last_sync = this.convertValues(source["last_sync"], null);
	        this.enabled = source["enabled"];
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

}

