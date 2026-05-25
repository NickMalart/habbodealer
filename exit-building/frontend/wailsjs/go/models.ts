export namespace main {
	
	export class ScheduleStatus {
	    targetUnix: number;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScheduleStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetUnix = source["targetUnix"];
	        this.enabled = source["enabled"];
	    }
	}

}

