export namespace application {
	
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
	    message: string;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new ConsoleLogDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptId = source["scriptId"];
	        this.scriptName = source["scriptName"];
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
	export class MemoryAreaDTO {
	    id: string;
	    displayName: string;
	    isBit: boolean;
	    size: number;
	    readOnly: boolean;
	    byteAddressing: boolean;
	    oneOrigin: boolean;
	
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
	    lastError: string;
	    errorAt: number;
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
	        this.lastError = source["lastError"];
	        this.errorAt = source["errorAt"];
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

}

