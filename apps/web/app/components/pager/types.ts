export interface Message {
  id: number;
  address: string;
  message: string;
  created_at: string;
  capcode_alias: string | null;
  capcode_icon: string | null;
  capcode_colour: string | null;
  agency_name: string | null;
  system_uuid: string | null;
  system_name: string | null;
}

export interface Agency {
  id: number;
  name: string;
  capcode_count: number;
}

export interface MessagePage {
  data: Message[];
  page?: number;
  per_page?: number;
  has_more: boolean;
}
