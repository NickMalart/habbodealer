export namespace main {
	
	export class WaveConfig {
	    minutes: number;
	    humanize: boolean;
	    running: boolean;
	    connected: boolean;
	    lastWaveAt?: string;
	    nextWaveInMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new WaveConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minutes = source["minutes"];
	        this.humanize = source["humanize"];
	        this.running = source["running"];
	        this.connected = source["connected"];
	        this.lastWaveAt = source["lastWaveAt"];
	        this.nextWaveInMs = source["nextWaveInMs"];
	    }
	}

}

