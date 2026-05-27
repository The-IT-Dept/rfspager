import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api, type CreateSystemInput } from "../services/api-client"

export function useSystems() {
  return useQuery({
    queryKey: ["systems"],
    queryFn: () => api.systems.list(),
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
  })
}

export function useMySystems() {
  return useQuery({
    queryKey: ["my-systems"],
    queryFn: () => api.systems.mine(),
    staleTime: 15 * 1000,
    refetchInterval: 30 * 1000,
  })
}

export function useSystem(uuid: string | undefined) {
  return useQuery({
    queryKey: ["system", uuid],
    queryFn: () => api.systems.get(uuid!),
    enabled: !!uuid,
    staleTime: 10 * 1000,
    refetchInterval: 30 * 1000,
  })
}

export function useCreateSystem() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateSystemInput) => api.systems.create(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["systems"] })
      qc.invalidateQueries({ queryKey: ["my-systems"] })
    },
  })
}
