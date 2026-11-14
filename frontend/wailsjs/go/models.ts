export namespace application {
	
	export class AppLogDTO {
	    id: number;
	    at: number;
	    level: string;
	    source: string;
	    target: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new AppLogDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.at = source["at"];
	        this.level = source["level"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.message = source["message"];
	    }
	}
	export class CapabilitiesDTO {
	    supportsUnitId: boolean;
	    unitIdMin?: number;
	    unitIdMax?: number;
	
	    static createFrom(source: any = {}) {
	        return new CapabilitiesDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supportsUnitId = source["supportsUnitId"];
	        this.unitIdMin = source["unitIdMin"];
	        this.unitIdMax = source["unitIdMax"];
	    }
	}
	export class ClientBrowseEntryDTO {
	    address: string;
	    name: string;
	    kind: string;
	    detail: string;
	    hasChildren: boolean;
	    selectable: boolean;
	    fields: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ClientBrowseEntryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.detail = source["detail"];
	        this.hasChildren = source["hasChildren"];
	        this.selectable = source["selectable"];
	        this.fields = source["fields"];
	    }
	}
	export class ConditionDTO {
	    field: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new ConditionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.value = source["value"];
	    }
	}
	export class OptionDTO {
	    value: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new OptionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	    }
	}
	export class FieldDTO {
	    name: string;
	    label: string;
	    description?: string;
	    type: string;
	    required: boolean;
	    default: any;
	    options?: OptionDTO[];
	    min?: number;
	    max?: number;
	    showWhen?: ConditionDTO;
	    category?: string;
	    browse?: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.options = this.convertValues(source["options"], OptionDTO);
	        this.min = source["min"];
	        this.max = source["max"];
	        this.showWhen = this.convertValues(source["showWhen"], ConditionDTO);
	        this.category = source["category"];
	        this.browse = source["browse"];
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
	export class ClientCommandDTO {
	    id: string;
	    displayName: string;
	    fields: FieldDTO[];
	    pollable: boolean;
	    wordValues: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClientCommandDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.fields = this.convertValues(source["fields"], FieldDTO);
	        this.pollable = source["pollable"];
	        this.wordValues = source["wordValues"];
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
	export class ClientResultValueDTO {
	    label: string;
	    value: string;
	    bit?: boolean;
	    word?: number;
	
	    static createFrom(source: any = {}) {
	        return new ClientResultValueDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	        this.bit = source["bit"];
	        this.word = source["word"];
	    }
	}
	export class ClientCommandLogDTO {
	    seq: number;
	    clientId: string;
	    clientName: string;
	    at: string;
	    request: string;
	    sent: string;
	    received: string;
	    elapsedMs: number;
	    status: string;
	    error?: string;
	    summary: string;
	    values: ClientResultValueDTO[];
	    label?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientCommandLogDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.clientId = source["clientId"];
	        this.clientName = source["clientName"];
	        this.at = source["at"];
	        this.request = source["request"];
	        this.sent = source["sent"];
	        this.received = source["received"];
	        this.elapsedMs = source["elapsedMs"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.summary = source["summary"];
	        this.values = this.convertValues(source["values"], ClientResultValueDTO);
	        this.label = source["label"];
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
	export class ClientDTO {
	    id: string;
	    name: string;
	    protocolType: string;
	    displayName: string;
	    settings: Record<string, any>;
	    connected: boolean;
	    polling: boolean;
	    subscribing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClientDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.protocolType = source["protocolType"];
	        this.displayName = source["displayName"];
	        this.settings = source["settings"];
	        this.connected = source["connected"];
	        this.polling = source["polling"];
	        this.subscribing = source["subscribing"];
	    }
	}
	export class ClientMappingSummaryDTO {
	    variables: number;
	    ok: number;
	    commError: number;
	    writePending: number;
	    invalid: number;
	
	    static createFrom(source: any = {}) {
	        return new ClientMappingSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.variables = source["variables"];
	        this.ok = source["ok"];
	        this.commError = source["commError"];
	        this.writePending = source["writePending"];
	        this.invalid = source["invalid"];
	    }
	}
	export class ClientPollItemDTO {
	    id: string;
	    commandId: string;
	    params: Record<string, any>;
	    format: string;
	    request: string;
	    summary: string;
	    status: string;
	    error?: string;
	    at?: string;
	    elapsedMs: number;
	    values: ClientResultValueDTO[];
	    valuesAt?: string;
	    okCount: number;
	    errorCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ClientPollItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.commandId = source["commandId"];
	        this.params = source["params"];
	        this.format = source["format"];
	        this.request = source["request"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.at = source["at"];
	        this.elapsedMs = source["elapsedMs"];
	        this.values = this.convertValues(source["values"], ClientResultValueDTO);
	        this.valuesAt = source["valuesAt"];
	        this.okCount = source["okCount"];
	        this.errorCount = source["errorCount"];
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
	export class ClientPollingDTO {
	    intervalMs: number;
	    running: boolean;
	    cycles: number;
	    lastCycleMs: number;
	    items: ClientPollItemDTO[];
	    failThreshold: number;
	    mapping?: ClientMappingSummaryDTO;
	
	    static createFrom(source: any = {}) {
	        return new ClientPollingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.intervalMs = source["intervalMs"];
	        this.running = source["running"];
	        this.cycles = source["cycles"];
	        this.lastCycleMs = source["lastCycleMs"];
	        this.items = this.convertValues(source["items"], ClientPollItemDTO);
	        this.failThreshold = source["failThreshold"];
	        this.mapping = this.convertValues(source["mapping"], ClientMappingSummaryDTO);
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
	export class ClientProtocolDTO {
	    protocolType: string;
	    displayName: string;
	    connectionFields: FieldDTO[];
	    commands: ClientCommandDTO[];
	    supportsSubscription: boolean;
	    monitoredItemFields: FieldDTO[];
	    requestDescription: string;
	    requestExample: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientProtocolDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.displayName = source["displayName"];
	        this.connectionFields = this.convertValues(source["connectionFields"], FieldDTO);
	        this.commands = this.convertValues(source["commands"], ClientCommandDTO);
	        this.supportsSubscription = source["supportsSubscription"];
	        this.monitoredItemFields = this.convertValues(source["monitoredItemFields"], FieldDTO);
	        this.requestDescription = source["requestDescription"];
	        this.requestExample = source["requestExample"];
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
	
	export class ClientSavedCommandDTO {
	    id: string;
	    name: string;
	    mode: string;
	    commandId?: string;
	    params?: Record<string, any>;
	    request: string;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientSavedCommandDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.mode = source["mode"];
	        this.commandId = source["commandId"];
	        this.params = source["params"];
	        this.request = source["request"];
	        this.summary = source["summary"];
	    }
	}
	export class ClientSavedCommandInputDTO {
	    name: string;
	    mode: string;
	    commandId: string;
	    params: Record<string, any>;
	    request: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientSavedCommandInputDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.mode = source["mode"];
	        this.commandId = source["commandId"];
	        this.params = source["params"];
	        this.request = source["request"];
	    }
	}
	export class ClientSubscriptionItemDTO {
	    id: string;
	    params: Record<string, any>;
	    summary: string;
	    status: string;
	    error?: string;
	    at?: string;
	    values: ClientResultValueDTO[];
	    valuesAt?: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new ClientSubscriptionItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.params = source["params"];
	        this.summary = source["summary"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.at = source["at"];
	        this.values = this.convertValues(source["values"], ClientResultValueDTO);
	        this.valuesAt = source["valuesAt"];
	        this.count = source["count"];
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
	export class ClientSubscriptionDTO {
	    intervalMs: number;
	    running: boolean;
	    state: string;
	    error?: string;
	    items: ClientSubscriptionItemDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ClientSubscriptionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.intervalMs = source["intervalMs"];
	        this.running = source["running"];
	        this.state = source["state"];
	        this.error = source["error"];
	        this.items = this.convertValues(source["items"], ClientSubscriptionItemDTO);
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
	
	
	export class ConfigVariantDTO {
	    id: string;
	    displayName: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigVariantDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	    }
	}
	export class ConsoleLogDTO {
	    scriptId: string;
	    scriptName: string;
	    level: string;
	    message: string;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new ConsoleLogDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptId = source["scriptId"];
	        this.scriptName = source["scriptName"];
	        this.level = source["level"];
	        this.message = source["message"];
	        this.at = source["at"];
	    }
	}
	export class DataTypeInfoDTO {
	    id: string;
	    displayName: string;
	    description: string;
	    wordCount: number;
	
	    static createFrom(source: any = {}) {
	        return new DataTypeInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.description = source["description"];
	        this.wordCount = source["wordCount"];
	    }
	}
	export class StructFieldDTO {
	    name: string;
	    dataType: string;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new StructFieldDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dataType = source["dataType"];
	        this.offset = source["offset"];
	    }
	}
	export class StructTypeDTO {
	    name: string;
	    fields: StructFieldDTO[];
	    wordCount: number;
	
	    static createFrom(source: any = {}) {
	        return new StructTypeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.fields = this.convertValues(source["fields"], StructFieldDTO);
	        this.wordCount = source["wordCount"];
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
	export class DataTypesDTO {
	    types: DataTypeInfoDTO[];
	    structTypes?: StructTypeDTO[];
	
	    static createFrom(source: any = {}) {
	        return new DataTypesDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.types = this.convertValues(source["types"], DataTypeInfoDTO);
	        this.structTypes = this.convertValues(source["structTypes"], StructTypeDTO);
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
	
	export class IntervalPresetDTO {
	    label: string;
	    ms: number;
	
	    static createFrom(source: any = {}) {
	        return new IntervalPresetDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.ms = source["ms"];
	    }
	}
	export class MappingTargetDTO {
	    protocolType: string;
	    displayName: string;
	    label: string;
	    isClient: boolean;
	    supportsNodePublishing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MappingTargetDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.displayName = source["displayName"];
	        this.label = source["label"];
	        this.isClient = source["isClient"];
	        this.supportsNodePublishing = source["supportsNodePublishing"];
	    }
	}
	export class MemoryAreaDTO {
	    id: string;
	    displayName: string;
	    isBit: boolean;
	    size: number;
	    readOnly: boolean;
	    byteAddressing: boolean;
	    oneOrigin: boolean;
	    shortName: string;
	    bitsOf?: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryAreaDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.isBit = source["isBit"];
	        this.size = source["size"];
	        this.readOnly = source["readOnly"];
	        this.byteAddressing = source["byteAddressing"];
	        this.oneOrigin = source["oneOrigin"];
	        this.shortName = source["shortName"];
	        this.bitsOf = source["bitsOf"];
	    }
	}
	export class MonitoringItemDTO {
	    id: string;
	    order: number;
	    protocolType: string;
	    memoryArea: string;
	    address: number;
	    bitWidth: number;
	    endianness: string;
	    displayFormat: string;
	
	    static createFrom(source: any = {}) {
	        return new MonitoringItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.order = source["order"];
	        this.protocolType = source["protocolType"];
	        this.memoryArea = source["memoryArea"];
	        this.address = source["address"];
	        this.bitWidth = source["bitWidth"];
	        this.endianness = source["endianness"];
	        this.displayFormat = source["displayFormat"];
	    }
	}
	export class NodeAddressDTO {
	    variableId: string;
	    address: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeAddressDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.variableId = source["variableId"];
	        this.address = source["address"];
	        this.error = source["error"];
	    }
	}
	export class NodePublishingDTO {
	    protocolType: string;
	    enabled: boolean;
	    accessMode: string;
	
	    static createFrom(source: any = {}) {
	        return new NodePublishingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.enabled = source["enabled"];
	        this.accessMode = source["accessMode"];
	    }
	}
	
	export class ProtocolInfoDTO {
	    type: string;
	    displayName: string;
	    variants: ConfigVariantDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ProtocolInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.displayName = source["displayName"];
	        this.variants = this.convertValues(source["variants"], ConfigVariantDTO);
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
	export class ProtocolMappingDTO {
	    protocolType: string;
	    memoryArea: string;
	    address: number;
	    endianness: string;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolMappingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.memoryArea = source["memoryArea"];
	        this.address = source["address"];
	        this.endianness = source["endianness"];
	    }
	}
	export class VariantDTO {
	    id: string;
	    displayName: string;
	    fields: FieldDTO[];
	
	    static createFrom(source: any = {}) {
	        return new VariantDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.fields = this.convertValues(source["fields"], FieldDTO);
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
	export class ProtocolSchemaDTO {
	    protocolType: string;
	    displayName: string;
	    variants: VariantDTO[];
	    capabilities: CapabilitiesDTO;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolSchemaDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.displayName = source["displayName"];
	        this.variants = this.convertValues(source["variants"], VariantDTO);
	        this.capabilities = this.convertValues(source["capabilities"], CapabilitiesDTO);
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
	export class ScriptDTO {
	    id: string;
	    name: string;
	    code: string;
	    intervalMs: number;
	    isRunning: boolean;
	    order: number;
	
	    static createFrom(source: any = {}) {
	        return new ScriptDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.code = source["code"];
	        this.intervalMs = source["intervalMs"];
	        this.isRunning = source["isRunning"];
	        this.order = source["order"];
	    }
	}
	export class ServerConfigDTO {
	    protocolType: string;
	    variant: string;
	    settings: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.variant = source["variant"];
	        this.settings = source["settings"];
	    }
	}
	export class ServerInstanceDTO {
	    protocolType: string;
	    displayName: string;
	    variant: string;
	    status: string;
	    supportsNodePublishing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerInstanceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocolType = source["protocolType"];
	        this.displayName = source["displayName"];
	        this.variant = source["variant"];
	        this.status = source["status"];
	        this.supportsNodePublishing = source["supportsNodePublishing"];
	    }
	}
	
	
	export class UnitIDSettingsDTO {
	    min: number;
	    max: number;
	    disabledIds: number[];
	
	    static createFrom(source: any = {}) {
	        return new UnitIDSettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.min = source["min"];
	        this.max = source["max"];
	        this.disabledIds = source["disabledIds"];
	    }
	}
	export class VariableDTO {
	    id: string;
	    name: string;
	    dataType: string;
	    value: any;
	    mappings?: ProtocolMappingDTO[];
	    nodePublishings?: NodePublishingDTO[];
	
	    static createFrom(source: any = {}) {
	        return new VariableDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.dataType = source["dataType"];
	        this.value = source["value"];
	        this.mappings = this.convertValues(source["mappings"], ProtocolMappingDTO);
	        this.nodePublishings = this.convertValues(source["nodePublishings"], NodePublishingDTO);
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
	export class VariableDeviceStatusDTO {
	    variableId: string;
	    clientId: string;
	    clientName: string;
	    state: string;
	    writePending: boolean;
	    error?: string;
	    at?: string;
	
	    static createFrom(source: any = {}) {
	        return new VariableDeviceStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.variableId = source["variableId"];
	        this.clientId = source["clientId"];
	        this.clientName = source["clientName"];
	        this.state = source["state"];
	        this.writePending = source["writePending"];
	        this.error = source["error"];
	        this.at = source["at"];
	    }
	}

}

